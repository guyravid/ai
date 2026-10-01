# Base Template

How to build a tool that satisfies [`../contract/CONTRACT.md`](../contract/CONTRACT.md), in any
language. The Go version of these patterns is [`patterns/go.md`](patterns/go.md); the wire format is
[`patterns/envelope.md`](patterns/envelope.md).

This is guidance, not law. Deviate freely on internal structure; never on observable behaviour. Where
this document and the contract disagree, the contract is right.

## §0 Where things belong

| If a statement… | It belongs in |
|---|---|
| can be checked by running the binary and reading its output | the contract |
| stops being true when the implementation changes, language held fixed | this file |
| stops being true when the language changes | `patterns/<language>.md` |
| is a schema, a worked example, or a codec | `patterns/envelope.md` |

## §1 Project shape

Modules by responsibility. Names are illustrative; the dependency direction is not.

```
entrypoint     # parse argv, refuse secret flags, dispatch; the only caller of exit
registry       # command definitions — the single source of truth
config         # settings resolution, profiles, provenance, platform paths
secrets        # credential resolution, file permission checks, the redactor
upstream       # HTTP client, retry policy, pagination adapter
shape          # --fields projection, empty-stripping, size caps, ordering
envelope       # assembly, serialization, the single write, exit mapping
datasets       # --all storage, sidecars, cleanup, dataset commands
render         # --human output, built from a finished envelope
teach          # orientation, contract, flags, config, and domain topics
diagnostics    # doctor and list-config
serve          # MCP server; compiled in only when enabled
```

```
entrypoint ─┬─> registry ──> upstream ──> secrets
            ├─> config ──> secrets
            ├─> shape
            ├─> envelope ──> render
            ├─> datasets ──> shape
            ├─> teach ──> registry, config
            ├─> diagnostics ──> config, secrets, upstream, datasets
            └─> serve ──> registry, envelope
```

Command handlers live in `registry` and never import `envelope`, `datasets`, or `render`. A handler
that can write output will eventually write two documents; a handler that can reach the dataset store
will eventually cache something the contract says is stateless.

## §2 The registry

One entry per command. Everything else is generated from this list.

```
Command {
  Name         "cards.list"           # dotted; argv is Name split on "."
  Description  one line, ≤160 chars   # tools --detail prints this verbatim
  Input        typed parameter struct  # inputSchema is derived from it
  Output       typed result struct     # outputSchema is derived from it
  Flags        param -> flag           # plus positional order
  Mutates      bool                    # changes upstream; drives mutates, readOnly, --confirm
  Annotations  readOnly, idempotent, openWorld, destructive
  Paged        bool
  Collectable  bool                    # implies Paged
  TimeWindow   bool                    # accepts --since/--until
  Fields       default[], available[]
  Sort         "dateLastActivity desc, id asc"
  Limits       default, max
  Errors       codes this command can produce
  Examples     []argv
  Handler      func(ctx, Input) (Output, Continuation, error)
}
```

Consumers of the registry:

| Consumer | Uses |
|---|---|
| argv parser | `Name`, `Flags`, `Input` |
| `tools`, `tools --detail`, `describe` | Everything except `Handler` |
| `teach` | Names, descriptions, examples, flags |
| `--help` | Flags and descriptions |
| MCP server | `Name`, `Description`, schemas, `Annotations`, `Handler` |

**Derive schemas from types, never write them by hand.** A hand-written schema is a second source of
truth that silently drifts from the struct it describes. If the language cannot derive a schema from
a type, generate the type from the schema instead.

**Validate at startup.** Fail before handling any command if:

- a name does not match `[a-z][a-z0-9-]*` per segment, or a domain command has one segment;
- a name starts with a reserved word, or two names collide;
- `readOnly` is not the opposite of `Mutates`, or a name starting with `write` is not mutating;
- a flag collides with a reserved flag, or a credential-shaped flag is declared;
- `Collectable` is set without `Paged`, or `TimeWindow` is set on a non-list command;
- an error code falls outside the contract's set and the declared tool-specific range;
- a `teach` topic is named `contract`, `flags`, or `config`.

**Read-only builds** exclude mutating entries from the registry at build time, not at runtime. The
commands then do not exist in the binary, so no bug can reach them. Keep a names-only list of the
mutating commands, compiled into every build, so a read-only build can answer a command line naming
one with `refused` and `writes_disabled` rather than `usage`.

## §3 Handlers

```
func(ctx, Input) (Output, Continuation, error)
```

A handler:

- returns typed data and, for list commands, a continuation describing how to fetch more;
- returns a typed domain error rather than an error code;
- does not print, build envelopes, read the environment, touch datasets, or exit.

Mapping errors to the contract's codes happens in one place, so it stays consistent:

| Observation | Code |
|---|---|
| argv failed to parse or validate | `usage` |
| Config unreadable, profile unknown, credential file too permissive, dataset directory unusable | `config` |
| Credential missing, or upstream 401 / 403 | `auth` |
| Upstream 404 on the addressed resource | `not_found` |
| Upstream 404 on the API root itself | `upstream` — a wrong base URL is not a missing record |
| Upstream 400 / 422 | `validation` |
| Upstream 409 / 412 | `conflict` |
| Upstream 429 after retries | `rate_limited` |
| Deadline elapsed | `timeout` |
| DNS, connect, TLS failure after retries | `network` |
| Other upstream failure, including 5xx after retries | `upstream` |
| Some pages or items succeeded, others failed | `partial` |
| Dataset id unknown or expired | `cache_miss` |
| Anything unmapped, including a recovered crash | `internal` |

## §4 Assembling the response

One function produces every response. Nothing else writes to stdout or calls exit.

Order matters:

1. Build the envelope from the handler's result.
2. Project `--fields`, then strip empty values (§8).
3. Order keys and records (§11).
4. Apply `--max-string` and `--max-depth` (§8).
5. Serialize, measure against `--max-bytes`, and drop records from the end until it fits (§8).
6. Pass the serialized bytes through the redactor (§6).
7. Write with a single write call, and check that it succeeded.
8. Run dataset cleanup (§10). This is after the write so it never delays the agent.
9. Exit with the mapped code.

When `--human` is set, step 6 feeds the renderer instead of stdout; the envelope is still built in
full first, so the two can never disagree (§12).

A crash anywhere is recovered at the top level and reported as `internal` with a valid envelope.
Signals are caught and reported as `canceled` (130 / 143).

## §5 Configuration

### §5.1 Paths

Use the platform's standard user-directory lookup for the config and cache bases, and fail with
`config` when it cannot determine one. Guessing, or falling back to a temporary directory, leaves the
operator's configuration somewhere nobody will look.

Expand a leading `~` yourself on every platform; the tool is not always started through a shell.

### §5.2 Resolution

Every setting goes through one resolver, which records where each value came from while resolving
it. Reconstructing provenance afterwards means implementing the chain twice, and the two will
disagree.

```
resolve(S):
  flag --s                           -> origin flag,          source "--s"
  env  <TOOL>_S_<PROFILE>            -> origin env_profile,   source that variable name
  env  <TOOL>_S                      -> origin env_default,   source that variable name
  file profiles.<profile>.s          -> origin file_profile,  source "config.json#profiles.<p>.s"
  file default.s                     -> origin file_default,  source "config.json#default.s"
  env  AGENTCLI_S                    -> origin family,        source that variable name
  builtin                            -> origin builtin,       source "builtin"
```

**Per setting, not per profile.** The bug to avoid is "if a profile is selected, load the profile;
otherwise load the defaults". It only shows up once a profile is partially specified, which is every
real profile.

Profile names become variable segments by uppercasing and replacing `-` with `_`. Validate the name
before building a variable name from it, and reject `FILE`.

The resolved structure is what `list-config` prints, what `teach config` is generated from, and what
`doctor`'s `config` check validates. One structure, three readers.

### §5.3 The config file

- Load the default path if the file exists; if `--config` or `<TOOL>_CONFIG` names a file, it must
  exist.
- Reject unknown members, with the member path in `details`.
- **Reject any credential member that holds a value.** Only `<name>_file` and `env_file` are allowed
  for credentials. Do this while loading, so the tool refuses to start rather than failing later in
  a way that looks like a network problem.
- Never write the file, and never create it. Without any write path, nothing can leak into it.

## §6 Secrets

### §6.1 Resolution

Per credential, first match wins:

```
<TOOL>_<NAME>_<PROFILE>          direct, profile scope
<TOOL>_<NAME>_FILE_<PROFILE>     file,   profile scope
<TOOL>_<NAME>                    direct
<TOOL>_<NAME>_FILE               file
config profiles.<p>.<name>_file / env_file
config default.<name>_file / env_file
```

Direct before file within each scope; profile scope before unscoped. Resolve once at startup, hold
the values in one place, and never read them again.

Validate lazily: resolve at startup, but raise `auth` only when a handler actually needs a missing
credential. Discovery, `teach`, and `list-config` must work with nothing configured.

When reading an `env_file`, keep only the declared credential keys (profile-suffixed first) and
discard every other line before the data leaves the parser. Parse conservatively: optional `export`,
`KEY=value`, matching surrounding quotes stripped, `#` comments and blank lines skipped, no variable
expansion.

### §6.2 File permissions

Check every credential file before reading it. With POSIX modes, require `0600` or `0400` and fail
with `config` otherwise, naming the fix (`chmod 600 <path>`). Without POSIX modes, warn and carry on,
and have `doctor` report `unverified`.

Create your own files (datasets, sidecars) with owner-only permissions set explicitly at creation.
Relying on the process default means one permissive environment widens them silently.

### §6.3 Secret flags

Scan argv for the forbidden flags **before** parsing. A parser error on an unknown flag would return
`usage`, and the operator would never learn that their key is now in shell history. Match both
`--flag value` and `--flag=value`, and never echo the value.

### §6.4 The redactor

Build it as soon as credentials resolve, before the first request. For each credential, register:
the value as-is; the value after URL encoding; the value after base64 encoding; the full authorization header built from
it; and every prefix of 12 or more characters. Replace every match with the same marker.

Apply it to the serialized output bytes, not to a data structure. A structure walk misses strings
nested inside untyped upstream JSON, which is exactly where an echoed key ends up. Wrap stderr and
the dataset writer with the same redactor.

Ignore credentials shorter than 8 characters when building the matcher, or a very short value would
redact large parts of legitimate output.

Give the credential its own type whose string and serialization forms print a placeholder. Then
even an accidental log statement cannot print it.

## §7 Upstream

The contract leaves retry policy to each tool. A reasonable default:

| Aspect | Recommendation |
|---|---|
| Retry on | 408, 425, 429, 500, 502, 503, 504, and transient transport failures |
| Never retry | Other 4xx, and any write (the contract forbids automatic write retries) |
| Attempts | 3 total |
| Backoff | Exponential from 250 ms, doubling, capped at 4 s, with random jitter (off under `--deterministic`) |
| `Retry-After` | Honour it exactly, capped by the remaining budget. If it exceeds the budget, stop and return `rate_limited` with `retry_after_ms` |
| Deadlines | `TIMEOUT` per request, `BUDGET` for the whole invocation including retries and page-following |
| Redirects | Refuse cross-host redirects; they would carry the credential elsewhere. Cap same-host redirects at 3 |
| Base URL override | Honour an override only for loopback hosts unless explicitly allowed, so a stray variable cannot send the credential to an arbitrary host |
| User-Agent | Tool name, tool version, contract version, OS and architecture. No hostname, username, or paths |
| Concurrency | One request at a time when paging. Parallel fan-out only for independent requests |

## §8 Shaping output

### §8.1 `--fields`

Parse the expression once, validate every path against the command's `fields.available`, and
project each record into an ordered map in the requested order. Unknown paths are `usage` with the
closest match; a known path missing from one record is skipped silently.

The subtractive form (`-path`) starts from `fields.default` and removes paths. Mixing plain and
`-` paths in one expression is `usage`.

### §8.2 Empty values

Strip `null`, `""`, `[]`, and `{}` recursively after projection. Keep `false` and `0`. Record
`meta.stripped_empty:true`.

### §8.3 Size caps

- Strings: cut at `--max-string` characters (not bytes, or multi-byte text breaks mid-character),
  append `…[+N chars]`, and record the path.
- Depth: replace anything deeper than `--max-depth` with `"<depth-elided>"`.
- Bytes: serialize, and if over `--max-bytes`, drop records from the end and re-measure. Binary
  search on the record count is faster than removing one at a time. Set `next_cursor` to resume at
  the first dropped record.
- Single object over the cap: shorten its longest strings first, repeatedly, until it fits.
- Never apply any of this to `tools`, `describe`, or `teach`.

### §8.4 Time windows

Parse `--since` and `--until` into UTC instants once, at the start, and pass them to the handler
already resolved. Resolve relative values against a single `now` captured at startup, so `--since
-1h --until now` is always exactly one hour. Reject anything the contract does not list, including
local times without an offset.

## §9 Paging

### §9.1 Filling the limit

```
fill(ctx, input, limit):
  records = []
  cont    = input.cursor or start
  pages   = 0
  while len(records) < limit:
      if pages == MAX_PAGES:        return records, cont, truncated("max_pages")
      if budget spent:              return records, cont, truncated("budget")
      batch, cont = adapter.fetch(ctx, cont, pageSize(limit - len(records)))
      records += batch; pages += 1
      if cont is end:               break
  return records[:limit], cont
```

Ask upstream for the smaller of the remaining count and its maximum page size, so the last page
does not over-fetch.

### §9.2 The upstream adapter

The adapter is the only place the API's own pagination style is visible.

| Upstream style | Recognised by | Continuation stored in the cursor |
|---|---|---|
| Opaque cursor | `next`, `next_cursor`, `after` in the response | The token, as-is |
| Offset | `offset` / `skip` parameters | The next offset |
| Page number | `page` parameter | The next page number |
| Link header | `Link: <…>; rel="next"` | The next URL |
| No paging at all | Everything returned at once | A local offset into the response |

A short page does not mean the end for opaque-cursor APIs; an absent token does. For offset and page
styles, a short page or a reached `total` does.

### §9.3 Cursors

Encode the continuation, the command name, and the resolved parameters into the cursor
(`patterns/envelope.md` §8). A cursor that must be combined with the original flags will, sooner or
later, be combined with different ones.

## §10 Datasets

### §10.1 Collecting

Use the **largest page size upstream allows** when collecting, regardless of `--limit`. `--limit`
sizes the response; the upstream page size is a separate number. Passing `--limit` through would turn
a 10-request collection into a 100-request one, and nothing in the output would show it.

Stream records to disk as pages arrive. Holding the whole set in memory defeats the point.

Apply projection and empty-stripping **before** writing each record, so the stored file and every
later read have the same shape.

Stop at `DATASET_MAX_RECORDS`, `DATASET_MAX_BYTES`, `MAX_PAGES` under `--all` (default 1000), or
`BUDGET` under `--all` (default 600 s), and mark the dataset incomplete with the reason.

### §10.2 Files

```
<dataset dir>/
  <id>.jsonl         # one record per line
  <id>.meta.json     # sidecar
  .cleanup           # modification time = last cleanup
  .cleanup.lock      # created exclusively; held only during cleanup
```

Sidecar:

```json
{"id":"3c9e2a4f-…","tool":"trello","command":"cards.list","params":{"board":"5f2a"},"fields":["id","name","list","due"],"sort":"dateLastActivity desc, id asc","window":null,"created_at":"2026-09-30T10:02:11Z","last_accessed_at":"2026-09-30T10:40:57Z","complete":true,"incomplete_reason":null,"record_count":4213,"bytes":612004,"index_stride":256,"index":[0,37218,74390],"contract_version":"1.0","tool_version":"0.3.0"}
```

- **`last_accessed_at` is a field in the sidecar, not the filesystem access time.** Containers
  commonly mount with access-time updates off, which would make every dataset look either never
  read or always stale. This detail fails silently in exactly the environment these tools run in.
- `index` holds the byte offset of every 256th line, so reading from offset 2000 is one seek plus at
  most 255 line reads.
- `params` and `command` let a `cache_miss` hint name the exact query to re-run.

### §10.3 Writing safely

Parallel invocations are normal, so:

1. Write `<id>.jsonl.tmp` in the same directory, sync it, rename it into place.
2. Then write the sidecar the same way.

The sidecar is the proof of existence. A reader that finds it knows the data file is complete, so
readers need no lock.

Update `last_accessed_at` with the same write-then-rename. Concurrent updates can lose one another;
the worst outcome is a dataset expiring slightly early, which is acceptable.

### §10.4 Cleanup

After the response is written:

```
skip if .cleanup is younger than 60 s and the directory is under budget
take .cleanup.lock without waiting; if it is held, skip (break it if older than 60 s)
delete datasets idle longer than DATASET_TTL
while total > 90% of DATASET_TOTAL_BYTES: delete the least recently read
touch .cleanup; release the lock
```

Evict to 90% rather than to exactly the budget, or the very next call starts cleaning again.

Delete the sidecar before the data file. A reader arriving in between gets a clean `cache_miss`
rather than a half-deleted file.

A cleanup failure becomes a `cleanup_failed` warning. It never fails the command.

### §10.5 Deterministic ids

Under `--deterministic`, derive the id from a hash of the tool, command, resolved parameters, window,
and fields. Report the path relative to the dataset directory. Always fetch fresh data regardless:
the id names the request; it does not cache its answer.

## §11 Determinism

- Use an ordered map type for every object you emit. Most languages' default maps iterate in an
  unspecified or randomized order.
- Keep upstream numbers as raw JSON text. Decoding into a floating-point type corrupts large
  integers and changes the formatting of decimals.
- Sort every list by the declared `sort`, then by a unique key as the tiebreak.
- Keep everything time- or request-specific behind `--timing`.
- For golden-file tests, run under `--deterministic` and compare bytes.

## §12 Human output

Render from the finished envelope, never by running a second code path through the handler. Then
`--human` cannot report a different outcome from the JSON.

| Response | Rendering |
|---|---|
| List | A table of the projected fields, with the row count and whether more exist |
| Object | Aligned `field: value` lines |
| `doctor` | One line per check: status word, name, detail |
| `list-config` | Name, value, source; credentials shown as `set` or `not set` |
| Error | `ERROR <code> (exit <n>): <message>`, then the hint |

Plain text only: no colour, no escapes, no terminal-width detection. Columns are aligned by padding,
which reads correctly in a terminal and in a captured log alike.

## §13 Writes

- Whether a command writes is a registry field (`Mutates`), not something parsed from its name. The
  name maps to argv like any other: `cards.create` becomes `cards create`, and an optional `write`
  prefix (`write.cards.create`) is just another segment.
- Derive `mutates`, `readOnlyHint`, and where `--confirm` applies from that one field.
- Build the outbound request first, then decide: without `--confirm`, return it as the preview; with
  `--dry-run`, return it as `data.preview`; otherwise send it. Building the request before deciding
  guarantees the preview is exactly what would be sent.
- Redact header values in the preview.
- Where upstream supports idempotency keys, derive one from the request body so a manual retry by
  the agent does not duplicate the change.

## §14 `teach`

| Topic | Source |
|---|---|
| orientation (bare `teach`) | Hand-written domain text wrapped around a generated list of contract commands and a generated pointer to `teach contract` |
| `contract` | `teach/shared.md.tmpl` from this bundle, vendored unchanged into each tool |
| `flags` | Generated from the reserved flag table and the registry |
| `config` | Generated from the settings structure (§5.2), including a complete example config file |
| domain topics | Hand-written Markdown, compiled into the binary |

Vendor the two templates (`teach/orientation.md.tmpl` and `teach/shared.md.tmpl`) into
each tool and compile them in. A test compares the vendored copies against this bundle's and fails
with a re-vendor instruction if they differ. Compiling in, rather than fetching, keeps the binary
self-contained; the drift test keeps every tool teaching the same contract.

Keep the orientation under 4 KB by measuring it in a test, not by eye.

## §15 Diagnostics

### §15.1 `doctor`

Run the checks as an ordered list, each receiving the results so far, so a check can report `skip`
when a prerequisite failed. Append every result unconditionally; the easy bug is skipping ahead past
a check whose prerequisite failed, which makes it vanish from the report.

- Give each check its own short deadline. A `doctor` that hangs for the full budget on an
  unreachable host is worse than one that fails in two seconds.
- For `auth`, use the cheapest authenticated read the API offers, ideally one that returns the
  caller's identity (`/members/me` on Trello).
- For `credentials`, report the source and permissions: "API_KEY from TRELLO_API_KEY_FILE
  (~/.secrets/trello.key, mode 0600)". Never the value, and never a prefix of it.

### §15.2 `list-config`

Print the resolved structure from §5.2 directly. Include every setting, set or not. For unset
credentials, include a hint listing every way to supply them.

## §16 MCP server mode

### §16.1 Ready without being on

The contract lets each tool decide whether to ship server mode. The substrate should always
implement it fully, so enabling it for a tool is a build flag, not a project:

- Command definitions are already MCP tool definitions (§2), so the server publishes the registry
  as-is.
- Tool arguments bind onto the same `Input` struct argv parsing produces, and call the same handler
  through the same response assembly (§4). There is no second path.
- Build without it by excluding the server package at compile time, so a build without MCP contains
  no listener code at all. `serve` in such a build returns `refused`.

### §16.2 HTTP hardening

| Concern | Approach |
|---|---|
| Binding | Parse the address; if the host is not loopback, require `--allow-remote`, a token, and a TLS decision (below), else fail with `config` before listening |
| TLS | For a remote bind, require either a certificate and key or `--tls-terminated-upstream`, never neither and never both. Check the key file's permissions like a credential file. Refuse protocol versions below TLS 1.2. Validate the certificate at startup (loads, matches the key, not expired) so a bad file fails immediately rather than on the first client |
| Token | Resolve like a credential; register it with the redactor; compare in constant time |
| Origin | Reject a request whose `Origin` is present and not loopback, with 403 |
| Body size | Cap request bodies at 1 MiB, answering 413 |
| Health | Serve `/health` before authentication, with only the fields the contract lists |
| Startup line | Transport, address, writes enabled, auth on or off, and the TLS mode: `https`, `plaintext on loopback`, or `plaintext; TLS terminated upstream (operator-declared)` |
| Sessions | Bound the number of sessions and expire idle ones |

## §17 Logging

Stderr only. Levels `off`, `error`, `warn`, `info`, `debug`; default `error`; `--verbose` sets
`debug`. Emit one JSON object per line, so combined output stays machine-filterable. Everything goes
through the redactor. Log header names only, except a short list of harmless headers, and mask query
parameters whose names suggest secrets.

## §18 Packaging

The contract says nothing about packaging, but these tools are only useful if they can be dropped
into wherever the agent runs. Aim for:

- **One self-contained file.** No runtime, interpreter, or package manager on the host.
- **Runs in a minimal container**, holding only the binary, as a non-root user.
- **Paths set explicitly in containers.** Minimal images often have no home directory, and the tool
  will refuse to guess (§5.1). Set `<TOOL>_CONFIG` and `<TOOL>_DATASET_DIR`, and mount the dataset
  directory writable.
- **Cross-compiled** for every platform the agent runtimes use.
- **Version stamped at build time** into `tool_version`, with the commit, so `version` never reports a
  number someone forgot to update.
- **Checksums published** with each release.

## §19 Testing

The contract's rules become a test matrix in
[`../skills/scaffold-api-cli/references/conformance-checklist.md`](../skills/scaffold-api-cli/references/conformance-checklist.md).
Implement it once in the substrate so every tool inherits it. The tests most worth insisting on:

| Test | Why |
|---|---|
| Golden envelopes under `--deterministic`, one per error code | Catches any drift in shape or exit mapping |
| MCP parity: the server's tool list equals `describe`, parameter for parameter | The only automated proof that the single source of truth still holds |
| Profile inheritance: a profile setting one value leaves the others at their lower-tier values | The per-setting bug only appears with partial profiles |
| A credential planted in an upstream response never reaches stdout, stderr, or a dataset | The reason these tools exist |
| A credential flag on argv is refused before parsing, and the value is not echoed | Same |
| A write without `--confirm` sends zero requests | Counted at a fake upstream, not inferred |
| `--all` issues fewer upstream requests than paging at the same `--limit` | Proves the largest page size is used |
| Paging a dataset to the end issues zero upstream requests | Proves reads come from disk |
| Dataset expiry and eviction against an injected clock | A test that sleeps for an hour gets skipped, and then nothing tests expiry |
| Parallel writers and readers of one dataset | No reader ever sees a partial file |
| `--human` exit code equals JSON exit code, for every error code | Rendering must never change the outcome |
| Orientation under 4 KB; `teach contract` identical to the bundle copy | Keeps the first call cheap and the shared text shared |

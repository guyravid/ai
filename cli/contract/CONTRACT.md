# Agent CLI Contract

**Contract version `1.4`**

A conforming tool is a command-line binary that gives an automated agent access to one remote API.
This document says what such a tool does and why. How to build one is in
[`../base/BASE_TEMPLATE.md`](../base/BASE_TEMPLATE.md); how to use and operate one is in
[`../AGENT.md`](../AGENT.md). Changes to this document are recorded in [`history/`](./history/).

## §0 About this document

Requirement levels (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) carry their RFC 2119 meanings.

This document binds only what can be observed from outside the binary: its arguments, its exit
codes, the bytes it writes, the files it creates, and the protocol it speaks in server mode. It names
no programming language.

| Term | Meaning |
|---|---|
| tool | One binary conforming to this contract |
| agent | The primary consumer: an automated process calling the tool through a shell or MCP. Every default is chosen for it |
| operator | A person who installs, configures, and troubleshoots the tool |
| upstream | The remote API the tool wraps |
| command | One operation the tool exposes, such as `cards.list` |
| envelope | The JSON document a command writes to stdout (§3) |
| dataset | A complete result set stored on local disk so it can be read in parts (§10) |
| profile | A named set of setting overrides, selected per invocation (§13) |
| `<tool>` | The binary name, for example `trello` |
| `<TOOL>` | The binary name uppercased, with every non-alphanumeric replaced by `_`, for example `TRELLO` |

## §1 Axioms

Every rule in this document traces back to one of these.

- **A1 — Output is context.** Everything the tool prints lands in an agent's context window and
  costs tokens. Small, predictable output is a correctness property, not a courtesy.
- **A2 — Nobody is watching.** Output never depends on whether a terminal is attached. There is no
  colour, no progress display, and no prompting.
- **A3 — Failures explain themselves.** Every failure carries a machine-readable class and a next
  step. A bare failure leads an agent to retry blindly or invent a workaround.
- **A4 — The command line is public.** Arguments are kept in shell history, transcripts, process
  listings, and hook logs. No secret is ever passed there.
- **A5 — Writes are declared and confirmed.** Every command that changes upstream state says so in
  discovery, and never runs without `--confirm`. An agent learns what a command does from the tool,
  not from guessing at its name.
- **A6 — The binary describes itself.** Everything an agent needs to use the tool is available from
  the tool. External documentation drifts; the binary cannot drift from itself.

## §2 Output streams

The tool:

1. MUST write exactly one JSON document to stdout per invocation: UTF-8, compact, followed by a
   single newline. The only exceptions are `teach` (§6.3), `--help` (§6.4), `--human` (§15), and
   server mode (§16), each of which is reached only by an explicit command or flag.
2. MUST write that document in a single write, never piece by piece.
   *Why:* a tool that is interrupted then leaves either nothing or a complete document, never broken
   JSON. The guarantee has a limit: when stdout is a pipe, the operating system accepts only what
   fits in the pipe's buffer (64 KiB on Linux and macOS) and blocks the write until the reader
   drains it. A kill that cannot be caught (SIGKILL, an out-of-memory kill), arriving while a larger
   document is blocked, cuts it off. The default `--max-bytes` (§8.1) fits within that buffer; a
   caller raising it should treat output that is not valid JSON, together with a non-zero exit, as
   an interrupted call. SIGINT and SIGTERM are caught, so a write already under way completes before
   the tool exits.
3. MUST write error envelopes to stdout, not stderr.
   *Why:* many callers capture only stdout. An error on stderr would look like an empty result.
4. MUST NOT require anything on stderr to interpret a result. Stderr carries diagnostics only,
   governed by the `LOG_LEVEL` setting and `--verbose`.
5. MUST NOT write terminal escape codes (colour, cursor movement) to stdout or stderr.
6. MUST produce identical stdout whether or not a terminal is attached. (A2)
7. MUST NOT read stdin, except in server mode over stdio.
8. MUST NOT prompt. Confirmation is given with `--confirm` (§11).

## §3 The envelope

### §3.1 Shape

Every command except those listed in §2.1 returns an envelope.

```json
{"ok":true,"tool":"trello","command":"cards.list","data":[{"id":"91bc","name":"Fix paging","list":"Doing"}],"meta":{"page":{"limit":25,"count":1,"total":1,"total_is_exact":true,"has_more":false,"next_cursor":null,"truncated":false},"fields":["id","name","list"],"stripped_empty":true,"sort":"dateLastActivity desc, id asc","contract_version":"1.4","tool_version":"0.3.0"}}
```

| Member | Rule |
|---|---|
| `ok` | Boolean. `true` on success, `false` on failure |
| `tool` | The binary name |
| `command` | The dotted command name (§5). `null` only when the arguments named no recognisable command |
| `data` | An array for list commands, an object for single-resource commands. `null` on failure, except for `partial` (the records retrieved) and `doctor` (the check report). Never wrapped in an extra key such as `items` |
| `error` | Present only on failure (§3.3) |
| `meta` | Always present (§3.2) |

No other top-level member is permitted. Command-specific content belongs in `data`.

An empty result is a success: `ok:true`, `data:[]`, `meta.page.count:0`, exit 0. It is never
`not_found`.

### §3.2 `meta`

`meta` MUST always carry `contract_version` and `tool_version`. Everything else appears only when it
applies:

| Member | Present when | Content |
|---|---|---|
| `page` | The command returns a list | Paging state (§9.2) |
| `window` | The command has a time dimension | The resolved time range (§8.5) |
| `fields` | A field projection applied | The fields returned, in order (§8.2) |
| `stripped_empty` | Empty-stripping applied | `true` (§8.3) |
| `elided_fields` | A value was shortened by a size cap | Paths of the shortened values (§8.4) |
| `sort` | The command returns a list | The sort order applied (§14) |
| `dataset` | `--all` was used | The stored dataset (§10.1) |
| `errors` | Exit code is `partial` | What failed (§4) |
| `dry_run` | `--dry-run` was given | `true` (§11.2) |
| `warnings` | Something is worth knowing but is not a failure | Array of `{code, message}` |
| `timing` | `--timing` was given | Durations and request counts (§14) |

`meta` MUST NOT contain wall-clock values, durations, request identifiers, or hostnames unless
`--timing` is given. *Why:* they make identical calls produce different output (§14), and they cost
context the agent rarely needs.

### §3.3 Failure

```json
{"ok":false,"tool":"trello","command":"cards.get","data":null,"error":{"code":"not_found","exit_code":5,"message":"No card with id 91zz is visible to this token.","retriable":false,"hint":"trello cards list --board <board-id> --limit 20","details":{"id":"91zz","upstream_status":404}},"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

| Member | Rule |
|---|---|
| `error.code` | One of the codes in §4. The set is closed |
| `error.exit_code` | MUST equal the process exit status. *Why:* some callers keep stdout but lose the exit status |
| `error.message` | One sentence, at most 512 characters. MUST NOT contain a secret |
| `error.retriable` | `true` when the same call may succeed later. It is not an instruction to retry immediately |
| `error.retry_after_ms` | Present when the wait time is known |
| `error.hint` | SHOULD be present. One line, and SHOULD be a command the agent can run as-is |
| `error.details` | A structured object, at most 2048 bytes serialized. MUST NOT contain a secret |

Rules for `details`:

- A `usage` error for an unknown flag or command MUST name it, and SHOULD include `did_you_mean`
  when a close match exists.
- Upstream response text copied into `details` MUST be cut to 512 characters and redacted (§12.3).
- A `refused` error for a missing `--confirm` MUST include `preview` (§11.2).

A usage error MUST NOT print help text. It returns the envelope, with a hint pointing to `describe`.

## §4 Error codes and exit codes

Each error code has exactly one exit code, and the pairing never changes once published (§18).

| Exit | `error.code` | Meaning | `retriable` |
|---|---|---|---|
| 0 | — | Success, including an empty or truncated result | — |
| 1 | `internal` | A defect in the tool, including a recovered crash | false |
| 2 | `usage` | Unknown command or flag, bad value, missing argument, malformed cursor or `--fields` | false |
| 3 | `config` | Configuration unreadable or invalid, unknown profile, credential file too permissive, dataset directory unusable | false |
| 4 | `auth` | Credential missing, or rejected by upstream (401, 403) | false |
| 5 | `not_found` | The addressed resource does not exist upstream (404) | false |
| 6 | `validation` | The call was well-formed, but upstream rejected its content (such as 400, 413, 422) | false |
| 7 | `conflict` | Upstream reported a conflict or failed precondition (409, 412) | false |
| 8 | `refused` | The tool declined: write without `--confirm`, write on a read-only build, server mode not built, secret on the command line | false |
| 9 | `rate_limited` | Upstream throttled the request (429) | true |
| 10 | `timeout` | A deadline passed before a usable response arrived | true |
| 11 | `network` | DNS, connection, or TLS failure | true |
| 12 | `upstream` | Any other upstream failure, including 5xx | true |
| 13 | `partial` | Some data was retrieved and some was not. `data` is populated and `meta.errors` lists what failed | true |
| 14 | `cache_miss` | The referenced dataset has expired, been evicted, or never existed | false |
| 15–31 | — | Reserved for future versions of this contract | — |
| 32–63 | — | Tool-specific. Each MUST be declared in `describe` | — |
| 130, 143 | `canceled` | Interrupted by SIGINT or SIGTERM | false |

Status codes in the table are examples. The error code follows from what upstream did, not from
the number alone: a response rejecting the request's content is `validation` whatever its status.

No exit code above 63 is permitted, apart from 130 and 143. *Why:* shells reserve that range, and
a caller would read those codes as a failure to run the binary at all.

`cache_miss` is distinct from `not_found` because the fixes differ: an expired dataset is rebuilt by
re-running the original query, while a missing upstream record cannot be rebuilt at all.

Truncation is never an error (§8.4).

How the tool retries upstream failures is the tool's own decision. Whatever the policy, the final
outcome MUST be reported with the matching code above.

## §5 Command naming

Command names are dotted: `<group>.<verb>`, optionally with further segments. The command line is the
name split on dots:

| Name | Command line |
|---|---|
| `cards.list` | `trello cards list` |
| `boards.get` | `trello boards get <id>` |
| `cards.create` | `trello cards create …` |

Rules:

1. Each segment matches `[a-z][a-z0-9-]*`. Domain commands have at least two segments.
2. Every command that changes upstream state MUST be declared mutating (§11.1). A name MAY start with
   `write`; a command whose name starts with `write` MUST be mutating.
3. The first segment MUST NOT be a reserved name (§17).

*Why dotted names:* they are valid MCP tool names, contain no spaces, and map to the command line
without a lookup table.

## §6 Discovery

Discovery is tiered. Each tier costs more context than the one before, and the agent chooses how
deep to go.

| Tier | Command | Returns |
|---|---|---|
| 1 | `tools` | Every command name |
| 2 | `tools --detail` | Plus a one-line description and whether it mutates |
| 3 | `describe <name>` | The full schema for one command |
| 4 | `describe` | The full schema for every command |

Every discovery command, `teach`, `doctor`, `list-config`, and `list-profiles` MUST work with no
configuration file and no credential present.

Discovery output MUST NOT be truncated, and no byte cap applies to it (§8.4). The tool emits it
complete or fails. *Why:* a truncated list silently hides that a command exists.

### §6.1 `tools`

`data` is a flat array of command names, sorted, covering every command the build exposes,
including the contract-provided ones (`serve` only when server mode is built):

```json
{"ok":true,"tool":"trello","command":"tools","data":["boards.get","boards.list","cards.create","cards.get","cards.list","dataset.clear","dataset.list","dataset.read","dataset.rm","dataset.stat","describe","doctor","list-config","teach","tools","version"],"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

`tools --detail` returns `[{name, description, mutates}]`. Each `description` MUST be one line of at
most 160 characters, with no flags, examples, or field lists. `mutates` MUST be `true` exactly when
the command changes upstream state (§11.1).

`tools --detail` SHOULD stay under 8 KB. Exceeding it suggests the command surface needs review.

### §6.2 `describe`

`describe` returns the tool's full self-description. `describe <name>` returns the same document
narrowed to one command. An unknown name is a `usage` error with `did_you_mean`.

```json
{"ok":true,"tool":"trello","command":"describe","data":{
  "tool":"trello","tool_version":"0.3.0","contract_version":"1.4",
  "writes_enabled":true,"mcp_enabled":false,
  "auth":{"credentials":[{"name":"API_KEY","env":"TRELLO_API_KEY"},{"name":"API_TOKEN","env":"TRELLO_API_TOKEN"}]},
  "exit_codes":[{"code":32,"name":"board_archived","retriable":false,"meaning":"The board is archived and read-only"}],
  "envelope_schema":{"…":"JSON Schema of §3, with data unconstrained"},
  "commands":[{
    "name":"cards.list","argv":["cards","list"],
    "description":"List cards on a board, most recently active first.",
    "annotations":{"readOnlyHint":true,"idempotentHint":true,"openWorldHint":true},
    "inputSchema":{"type":"object","additionalProperties":false,
      "properties":{"board":{"type":"string","description":"Board id"},
                    "list_id":{"type":"string","description":"Only cards in this list"}},
      "required":["board"]},
    "outputSchema":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}}}},
    "x-cli":{"flags":{"board":"--board","list_id":"--list-id"},"positional":[],"paged":true,"collectable":true,"time_window":false,
             "errors":["usage","auth","not_found","rate_limited","network","upstream"]},
    "fields":{"default":["id","name","list"],"available":["id","name","list","desc","due","labels","members","url","dateLastActivity"]},
    "sort":"dateLastActivity desc, id asc",
    "limits":{"default_limit":25,"max_limit":100},
    "examples":[{"argv":["cards","list","--board","5f2a","--fields","id,name,due"],"description":"Cards on a board with due dates"}]
  }]
}}
```

Each command entry is an MCP tool definition (`name`, `description`, `inputSchema`, `annotations`)
plus the members below. *Why MCP's shape:* agents already understand it, and server mode (§16) can
publish the same entries unchanged.

| Member | Rule |
|---|---|
| `argv` | The command line path |
| `outputSchema` | Constrains `data` only, never the whole envelope |
| `annotations.readOnlyHint` | `true` exactly when the command is not mutating; always the opposite of `mutates` in `tools --detail` |
| `x-cli.flags` | Maps each parameter to its flag |
| `x-cli.positional` | Parameters given by position, in order |
| `x-cli.paged` | Whether the command returns a list (§9) |
| `x-cli.collectable` | Whether `--all` is supported (§10) |
| `x-cli.time_window` | Whether `--since` and `--until` apply (§8.5) |
| `x-cli.errors` | The error codes this command can produce |
| `fields` | Default and available field paths (§8.2) |
| `sort` | The declared order of list output (§14) |
| `limits` | The default and maximum `--limit` |

`outputSchema` describes a record as upstream provides it, before shaping. `--fields` (§8.2),
empty-stripping (§8.3), and the size caps (§8.4) remove or replace values on every call, so an
`outputSchema` MUST NOT forbid what they produce:

- No object schema lists `required` properties or sets `additionalProperties` to `false`.
- Every object or array schema nested inside a record also admits the string `"<depth-elided>"`. A
  record itself (each element of a list, or `data` for a single resource) is never elided.
- No string schema sets `maxLength`, `pattern`, `enum`, or `const`, since a shortened string ends in
  `…[+N chars]`.

*Why:* MCP clients may validate structured results against the published schema (§16.2). A schema
stricter than the shaping rules turns a correct, shaped response into a client-side error.

`envelope_schema` MUST appear once, not per command. A command's full output schema is the envelope
schema with `data` replaced by `{"anyOf":[<outputSchema>,{"type":"null"}]}`, since a failure carries
`data:null` (§3.1).

No parameter may be marked as, or carry, a secret (§12.2).

### §6.3 `teach`

`teach` writes Markdown, not an envelope, and exits 0. *Why:* prose escaped inside JSON costs more
tokens and reads worse.

`teach` is tiered like `tools`:

| Call | Content |
|---|---|
| `teach` | A short orientation to this tool |
| `teach contract` | Everything shared by every tool under this contract, in one read |
| `teach --list` | The available topics, one line each |
| `teach <topic>` | One topic in full |
| `teach <topic> <item>` | One item within a topic |

**Bare `teach`** is where an agent meeting this tool for the first time starts, so it MUST stay
cheap. It SHOULD stay under 4 KB. It MUST:

1. Explain in a few sentences which API the tool covers and what an agent would use it for.
2. Name each command the contract itself provides, each with the question it answers: `tools`,
   `describe`, `teach`, `doctor`, `list-config`, `list-profiles`, plus the `dataset` commands in
   builds that have them.
3. Name the few domain commands that answer the most common questions. It MUST NOT list every
   command; that is what `tools` is for.
4. State the facts a first-time caller would otherwise get wrong.
5. Point onward by name: `teach contract` for the shared baseline, `tools` for the full command list,
   `describe <name>` for a schema, and `teach --list` for other topics.

**`teach contract`** covers what does not vary between tools: the envelope, the error codes and how
to recover from each, discovery, paging, datasets, field projection and size caps, write gating, and
how configuration and credentials resolve. Apart from the tool's name and environment prefix, it
MUST be identical across every tool. An agent that has read it once for any tool knows it for all.

**Two topics are always present and always generated:** `flags` and `config`.

- `flags` lists every reserved flag (§17) on this build. `teach flags <name>` MUST answer for every
  one of them.
- `config` MUST cover: every setting, with its default, its environment variable, and its flag where
  one exists; the precedence order (§13.2); how profiles work; where files live (§13.1); how
  credentials resolve (§12.1), stating plainly that credential references in the config file are
  **paths**, never values; and a complete example configuration file with at least one profile
  that sets more than one value. `teach config <aspect>` MUST let a reader open one of these alone.

A tool author MUST NOT define topics named `contract`, `flags`, or `config`.

Anything derivable from the tool's own definitions — command names, flags, defaults, settings,
examples — MUST be generated from the same definitions that drive `describe`, so it cannot drift from
the binary. Domain knowledge that cannot be derived, such as query idioms and known traps, MUST be
compiled into the binary, never fetched.

Every command named anywhere in `teach` output MUST exist in `tools`. An unknown topic or item is a
`usage` error with `did_you_mean`.

### §6.4 `--help`

`--help` and `-h` write human-readable text to stdout and exit 0. The last line MUST be:

```
# machine-readable: <tool> describe <name>
```

## §7 Diagnostics

### §7.1 `doctor`

`doctor` answers one question: can this tool do its job from here? It returns `data.checks`, an
ordered array of `{name, status, detail}`, where `status` is `pass`, `warn`, `fail`, or `skip`.

It MUST run at least these checks, in this order:

| Check | Confirms |
|---|---|
| `config` | The config file, if any, loads and validates, and the selected profile exists |
| `credentials` | Every required credential resolves. Reports where it came from and, for files, whether permissions were verified (§12.4). Never the value |
| `network` | The upstream host resolves and a TLS connection succeeds at TLS 1.2 or later |
| `auth` | One authenticated, read-only request succeeds. Reports the identity upstream returns, if it returns one |
| `clock` | Local time is within 60 seconds of upstream's `Date` header. Larger skew is `warn` |
| `datasets` | The dataset directory exists or can be created, and is writable |
| `writes` | Reports whether this build allows writes. Never tests a write |

A check that cannot run because an earlier one failed MUST be reported as `skip`, never omitted.
*Why:* an omitted check reads as one that passed.

`doctor` MUST NOT change upstream state. It exits 0 when no check is `fail`, and otherwise with the
exit code matching the first failing check. On failure it returns the error envelope with
`data.checks` still populated, since the full report is the point of the command.

There is no separate `whoami` command. Identity is reported by the `auth` check.

### §7.2 `list-config`

`list-config` reports every setting the tool reads and where each value came from. It MUST NOT
contact upstream, and it MUST succeed with nothing configured.

```json
{"ok":true,"tool":"trello","command":"list-config","data":{"profile":"work","config_file":"/Users/g/Library/Application Support/agentcli/trello/config.json","settings":[
  {"name":"LIMIT","value":50,"source":"TRELLO_LIMIT","origin":"env_default","default":25},
  {"name":"DATASET_TTL","value":"4h","source":"config.json#profiles.work.dataset_ttl","origin":"file_profile","default":"1h"},
  {"name":"TIMEOUT","value":"30s","source":"builtin","origin":"builtin","default":"30s"},
  {"name":"API_KEY","value":null,"set":true,"source":"TRELLO_API_KEY_WORK","origin":"env_profile","default":null},
  {"name":"API_TOKEN","value":null,"set":false,"source":"builtin","origin":"builtin","default":null,
   "hint":"Set TRELLO_API_TOKEN, or TRELLO_API_TOKEN_FILE=<path to a file holding the token>, or api_token_file=<path> in the config file"}
]},"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

| Member | Rule |
|---|---|
| `settings` | Every setting, including unset ones. *Why:* a setting missing from the list is indistinguishable from one the tool does not have |
| `source` | The exact origin: a flag, a full environment variable name, a pointer into the config file, or `builtin` |
| `origin` | Which precedence tier won (§13.2) |
| `default` | The built-in default, whether or not it is in effect |
| `profile`, `config_file` | The active profile and config file, or `null` |

For a credential, `value` MUST be `null` and `set` MUST be present. `source` still names where it
came from. When a credential is unset, a `hint` SHOULD name every way to supply it.

`list-config --schema` returns the JSON Schema of the config file in `data.schema`.

### §7.3 `list-profiles`

`list-profiles` lists the profiles the tool can run with, so an agent can choose a `--profile`
without reading configuration files or environment variables. It MUST NOT contact upstream, MUST
succeed with nothing configured, and MUST NOT be truncated.

```json
{"ok":true,"tool":"trello","command":"list-profiles","data":[
  {"name":"default","active":true,"declared_in":[]},
  {"name":"eu_west","active":false,"declared_in":["environment"]},
  {"name":"work","active":false,"declared_in":["config_file","environment"]}
],"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

| Member | Rule |
|---|---|
| `name` | The value `--profile` takes. `default` stands for running without `--profile` |
| `active` | `true` for the profile this invocation selected (§13.3), or for `default` when none is selected |
| `declared_in` | Where the profile is declared (§13.3): `config_file`, `environment`, or both. Empty for `default` |

Rules:

1. When no profile is declared, `data` is `[]`: the tool runs only without `--profile`.
2. Otherwise `default` comes first, then every declared profile, sorted by name. *Why:* listing
   `default` beside the others makes it plain that running with no profile is one of the choices.
3. A profile declared in the config file is listed under its name there. A profile declared only by
   `<TOOL>_<SETTING>_<PROFILE>` variables is listed under the variable's profile segment in
   lowercase (`TRELLO_TIMEOUT_EU_WEST` lists `eu_west`), and that name selects it. A file entry and
   variables that map to the same segment are one profile.
4. Names only: never a setting's value, and never a credential.
5. A selected profile that is not declared does not fail `list-profiles`: no entry is active, and a
   warning names the profile. *Why:* a mistyped profile name is exactly when the list is needed.

## §8 Context discipline

A command called with no flags MUST return a bounded amount of output. (A1)

### §8.1 Defaults

| Control | Default | Maximum | Notes |
|---|---|---|---|
| `--limit` | 25 | per command, in `describe` | Above the maximum is clamped, with a warning. Never an error |
| `--max-bytes` | 32768 | 1048576 | Size of the whole encoded document |
| `--max-string` | 2048 | 65536 | Characters per string value |
| `--max-depth` | 8 | 32 | Nesting depth of `data` |
| `--max-pages` | 10 | 100 | Upstream pages fetched per invocation (§9.1) |
| `--since` | `-24h` | — | On commands with a time dimension (§8.5) |
| Empty-stripping | on | — | `--keep-empty` turns it off |

### §8.2 `--fields`

`--fields` chooses which fields each record contains.

```
fields  = item *("," item)
item    = "*" | path | "-" path
path    = name *("." name) ["." "*"]
```

| Form | Meaning |
|---|---|
| `--fields id,name,due` | Exactly these fields, in this order |
| `--fields -desc` | The default fields, minus `desc` |
| `--fields '*'` | Every field upstream returns |
| `--fields badges.*` | Every field under `badges` |

Rules:

1. Adding and removing in one expression is a `usage` error.
2. Any other syntax — wildcards mid-path, array indexes, JSONPath — is a `usage` error, with
   `details.available_fields`.
3. A path not in the command's `fields.available` is a `usage` error with `did_you_mean`.
4. A path that is available but missing from a particular record is omitted from that record.

*Why:* projection is the agent's main tool for spending context only on what it needs.

### §8.3 Empty values

By default, `null`, `""`, `[]`, and `{}` are removed from records, and `meta.stripped_empty` is
`true`. `false` and `0` are kept: they are values, not absences. `--keep-empty` keeps everything.

### §8.4 Size caps and truncation

When a list response would exceed `--max-bytes`, the tool MUST drop whole records from the end until
it fits, and MUST set `meta.page.truncated:true`, `truncated_reason:"max_bytes"`, and `dropped` to the
number removed. `next_cursor` then points at the first dropped record. The output MUST remain valid
JSON and MUST NOT cut a record in half.

A single-object response that exceeds the cap is kept, and its largest strings are shortened until
it fits. Each shortened path is listed in `meta.elided_fields`. `data` MUST NOT become `null` because
of size.

A string longer than `--max-string` is shortened to its first characters followed by
`…[+N chars]`, and its path listed in `meta.elided_fields`. A subtree deeper than `--max-depth` is
replaced by `"<depth-elided>"`.

A list response that has records MUST carry at least one. When not even one whole record fits, the
tool MUST keep the first and shorten its largest strings, as for a single object, listing each
shortened path in `meta.elided_fields`. If it still does not fit, the cap is ignored for that
response, which carries the one record, and `truncated_reason` is `"max_bytes_below_minimum"`.
`next_cursor` points at the record after it. A response with no records whose envelope alone
exceeds the cap is likewise sent whole, with the same `truncated_reason`. *Why:* a response with no
records and a cursor pointing at the same record would send a caller following the cursor round the
same request forever.

Truncation is exit 0 and `ok:true`. None of this section applies to discovery or `teach` output.

### §8.5 Time windows

A command whose results have a time dimension MUST accept `--since` and `--until`, MUST default to
`--since -24h`, and MUST report the resolved range:

```json
"window":{"since":"2026-09-29T09:00:00Z","until":"2026-09-30T09:00:00Z","source":"default"}
```

`source` is `flag`, `default`, or `cursor`. *Why:* without it, a default 24-hour window looks the
same as a complete search.

Accepted formats:

| Form | Example | Notes |
|---|---|---|
| Relative | `-2h`, `-30m`, `-7d` | Units `s m h d w`. The leading `-` is required |
| RFC 3339 with offset | `2026-09-29T14:00:00+02:00` | |
| Date | `2026-09-29` | Midnight UTC |
| Epoch | `1790000000`, `1790000000000` | Exactly 10 digits (seconds) or 13 (milliseconds) |
| `now` | `now` | `--until` only |

Everything else — a date-time without an offset, `+2h`, `2h`, natural language — is a `usage` error.
*Why:* guessing a timezone gives answers that look right and are not. `--since` MUST be earlier than
`--until`. All times in output are RFC 3339 UTC with a `Z` suffix.

## §9 Paging

### §9.1 Filling the limit

The tool, not the agent, pages through upstream to satisfy `--limit`. With `--limit 60` against an
upstream that returns 20 records per page, the tool makes three upstream requests and returns 60
records in one envelope.

It stops at whichever comes first: `--limit` satisfied, upstream exhausted, `--max-pages` reached,
`--max-bytes` reached, or the invocation's time budget spent. Stopping at a cap before `--limit` is
satisfied is truncation: exit 0, `meta.page.truncated:true`, with the reason.

Upstream pages MUST be fetched one after another, not in parallel.

### §9.2 `meta.page`

Every list response carries `meta.page`, with the same members every time:

```json
"page":{"limit":60,"count":60,"total":214,"total_is_exact":true,"has_more":true,"next_cursor":"eyJ2IjoxLCJjIjoiY2FyZHMubGlzdCJ9","truncated":false}
```

| Member | Meaning |
|---|---|
| `limit` | The `--limit` actually applied, after clamping |
| `count` | Records in `data` |
| `total` | Total matching records, or `null` when upstream does not say |
| `total_is_exact` | `false` when `total` is an estimate or `null` |
| `has_more` | Whether records exist beyond this response |
| `next_cursor` | The cursor for the next records. `null` exactly when `has_more` is `false` |
| `truncated` | `true` when a cap stopped the tool before `limit` was reached |
| `truncated_reason` | `max_pages`, `max_bytes`, `max_bytes_below_minimum`, or `budget`. Present only when truncated |
| `dropped` | Records removed by the byte cap. Present only when non-zero |

### §9.3 Cursors

A cursor is an opaque string. The agent passes it back unchanged with `--cursor`, and the cursor
alone MUST be enough to continue: the agent MUST NOT need to repeat the original parameters.

A cursor names the command that issued it. Passing it to a different command is a `usage` error. A
malformed cursor is a `usage` error. A well-formed cursor naming a dataset that no longer exists is
`cache_miss`.

## §10 Datasets

When the agent knows it wants a whole result set, it asks the tool to fetch everything once and
store it locally. It then reads the stored copy in parts, at no further upstream cost and without
ever pulling the whole set through stdout. (A1)

### §10.1 Collecting with `--all`

A command whose `describe` entry has `x-cli.collectable:true` MUST accept `--all`. The tool then
fetches the complete result set, stores it as a dataset, and returns the first part in `data`, sized
by `--limit`, `--fields`, and `--max-bytes`. The complete stored set is summarised in `meta.dataset`:

```json
"dataset":{"id":"3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34","path":"/Users/g/Library/Caches/agentcli/trello/datasets/3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34.jsonl","record_count":4213,"bytes":2894120,"complete":true,"offset":0,"returned":25,"ttl_seconds":3600,"read_hint":"trello dataset read 3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34 --offset 25 --limit 100"}
```

| Member | Rule |
|---|---|
| `id` | A random UUID (see §14 for `--deterministic`) |
| `path` | Absolute path to the stored file. The agent MAY read it directly with line-oriented tools. Omitted over MCP HTTP (§16.3) |
| `complete` | `false` whenever a cap stopped collection early; `incomplete_reason` then says which |
| `offset`, `returned` | Where the returned part sits within the whole |
| `ttl_seconds` | How long the dataset survives without being read |
| `read_hint` | SHOULD be a command the agent can run as-is |

Rules:

1. The dataset MUST be JSON Lines: one record per line, no enclosing array.
   *Why:* it can be written and read incrementally, addressed by line, and processed by standard
   line tools.
2. Records MUST be stored after `--fields` and empty-stripping are applied, so every later read
   returns records of the same shape.
3. A dataset MUST be written even when everything fit in the first response, so `--all` always
   returns a `path`.
4. `meta.page.next_cursor` in the `--all` response MUST be a cursor for `dataset read`.
5. Reaching a collection cap is exit 0 with `complete:false`. An upstream failure partway through is
   `partial` (exit 13): the records collected so far are kept, `complete` is `false`, and
   `meta.errors` says what failed.

### §10.2 Storage and retention

Datasets live in the dataset directory (§13.1). If that directory cannot be created or written, the
command fails with `config`. The tool MUST NOT store the dataset elsewhere, and MUST NOT carry on
without storing it. *Why:* silently skipping storage would hand the agent a handle to data that does not exist.

Each dataset is a `<id>.jsonl` file plus a `<id>.meta.json` sidecar recording at least the command,
creation time, last-access time, completeness, record count, size, fields, sort, and window.

| Rule | Detail |
|---|---|
| Sliding lifetime | A dataset expires `DATASET_TTL` after it was last read. Every `dataset read` and `dataset stat` resets the clock |
| Last access | MUST be recorded in the sidecar. Filesystem access times MUST NOT be relied on. *Why:* many mounts do not update them |
| Size budget | When the directory exceeds `DATASET_TOTAL_BYTES`, expired datasets go first, then the least recently read |
| Cleanup | Runs opportunistically during normal invocations. No background process. A cleanup failure is a warning, never a failed command |
| Permissions | Directory owner-only, files owner-only, set explicitly at creation (§12.4) |
| Redaction | Everything written to a dataset passes through the redactor (§12.3) |

Datasets hold upstream data at rest and may fall under the host's data-handling rules.

### §10.3 `dataset` commands

A tool that supports `--all` MUST provide:

| Command | Behaviour |
|---|---|
| `dataset read <id> [--offset N] [--limit M] [--fields …]` | Returns part of a dataset as a normal list envelope. Never contacts upstream. Resets the lifetime |
| `dataset list` | `[{id, command, record_count, bytes, complete, age_seconds}]` |
| `dataset stat <id>` | The sidecar contents. Resets the lifetime |
| `dataset rm <id>` | Deletes one dataset |
| `dataset clear --confirm` | Deletes every dataset. Without `--confirm` it is `refused` |

An unknown or expired `<id>` is `cache_miss`, with a hint naming the query to re-run.

`--fields` on `dataset read` can only narrow what was stored. Asking for a field the dataset does not
hold is a `usage` error listing what it does hold.

Dataset commands are not mutating: they change only local files, not upstream.

## §11 Writes

### §11.1 Declaring writes

Every command that changes upstream state MUST be declared mutating: `mutates:true` in
`tools --detail`, `readOnlyHint:false` in `describe` (§6). Its name follows the same rules as any
other command:

```
trello cards list --board 5f2a                                       # read
trello cards create --list-id 61a0 --name "Fix paging" --confirm     # write
```

A name MAY start with `write` (`trello write cards create …`). If it does, the command MUST be
mutating, so the prefix never misleads.

*Why:* the declaration is where both an agent and an MCP client look, and `--confirm` (§11.2) gates
every run whatever the command is called. The cost: a permission rule that matches only the start
of a command line cannot tell reads from writes when names carry no prefix. Where reads and writes
must be separated by something other than `--confirm`, use a read-only build (§11.3), or deny each
mutating command by name.

### §11.2 `--confirm` and `--dry-run`

- A mutating command MUST refuse to run without `--confirm`: exit `refused`, and `error.details.preview`
  MUST show the exact request the tool was about to make — `{method, url, headers, body}`, with
  header values redacted.
- Every command MUST accept `--dry-run`. It sends no mutating request, returns `ok:true` with the
  would-be request in `data.preview`, and sets `meta.dry_run:true`.
- `--dry-run` together with `--confirm` is a dry run. Nothing is sent.

### §11.3 Read-only builds

A tool MAY be built without writes. In such a build:

- No mutating command appears in `tools`, `describe`, `teach`, or the server's tool list.
- A command line naming a mutating command is `refused`, with `details.reason:"writes_disabled"`. The
  tool still recognises the names of its mutating commands for this purpose. *Why:* "not built" and
  "not a command" call for different responses from the caller.
- `describe`, `doctor`, `version`, and the server's health endpoint report `writes_enabled:false`.

### §11.4 Retrying writes

The tool MUST NOT retry a write automatically after a timeout or an upstream failure. *Why:* the
first attempt may have succeeded, and a retry would duplicate the change. After a write times out, the
tool returns `timeout` with `details.write_state:"unknown"` and a hint to check upstream before trying
again. Where upstream supports idempotency keys, the tool SHOULD send one.

## §12 Secrets

### §12.1 Where credentials come from

A tool declares each credential it needs by name, such as `API_KEY` and `API_TOKEN`. For each one,
the first match below wins:

| Order | Source | Example with `--profile work` |
|---|---|---|
| 1 | Profile-scoped environment variable | `TRELLO_API_KEY_WORK` |
| 2 | Profile-scoped file variable, naming a file whose trimmed contents are the value | `TRELLO_API_KEY_FILE_WORK` |
| 3 | Environment variable | `TRELLO_API_KEY` |
| 4 | File variable | `TRELLO_API_KEY_FILE` |
| 5 | Config file, active profile: `<name>_file`, or an `env_file` containing the key | `profiles.work.api_key_file` |
| 6 | Config file, `default` section, same forms | `default.env_file` |

Within one scope, the direct variable is checked before the file variable. *Why:* environment
variables are the easiest thing for a deployment to override. Without a profile, steps 1, 2, and 5
are skipped.

An `env_file` holds `KEY=value` lines. The tool reads only the keys of credentials it declares,
profile-suffixed keys first, and ignores every other line.

A missing credential is `auth`, with a hint naming every way to supply it.

### §12.2 Never on the command line

No credential is ever accepted as an argument. (A4)

No tool may define any of these flags: `--api-key`, `--key`, `--token`, `--password`, `--secret`,
`--credential`. If one appears on a command line, the tool MUST exit `refused` with
`details.reason:"secret_on_argv"`, and the message MUST tell the operator to rotate the credential,
since it has already been recorded wherever the command line was logged.

Flags that take a **path** to a secret file, such as `--token-file`, are permitted.

### §12.3 Redaction

The tool builds a redactor as soon as credentials are resolved, before any request is sent. It MUST:

1. Cover stdout, stderr, and every dataset file.
2. Match each credential in its raw form, URL-encoded, and base64-encoded; the full authorization
   header value built from it; and any run of 12 or more characters taken from the start of it.
3. Apply to upstream text copied into errors, since some APIs echo the credential back.
4. Replace every match with the same fixed marker, regardless of which secret matched.

In `--verbose` logs, query parameters whose names suggest secrets (key, token, password, secret,
signature, auth) MUST be masked, and headers MUST be logged by name only, except for a fixed list of
harmless headers such as `Content-Type`, `Date`, and rate-limit headers.

No `--fields` path can reach a credential.

### §12.4 Protecting credential files

Every file the tool reads a credential or token from — `_FILE` variables, `<name>_file`, `env_file`,
the server token file, the server's TLS private key — MUST grant access to its owner and nobody else:

- **Where the platform has POSIX permissions:** the file MUST be mode `0600` or `0400`. Anything
  looser is `config`, with a hint giving the command that fixes it.
- **Where it does not:** the tool MUST NOT fail. It MUST add a warning naming the file and saying the
  check was not performed, and `doctor` MUST report the file's permissions as `unverified`, never as
  passing.

Files the tool creates, including datasets, MUST be created owner-only explicitly, not left to the
process default.

## §13 Configuration

### §13.1 Where files live

Paths follow the platform's own conventions for user configuration and cache data:

| Platform | Config base | Cache base |
|---|---|---|
| Linux and other Unix | `$XDG_CONFIG_HOME`, else `~/.config` | `$XDG_CACHE_HOME`, else `~/.cache` |
| macOS | `$XDG_CONFIG_HOME` if set, else `~/Library/Application Support` | `$XDG_CACHE_HOME` if set, else `~/Library/Caches` |
| Windows | `%APPDATA%` | `%LOCALAPPDATA%` |

Within those:

```
<config base>/agentcli/<tool>/config.json      # config file
<cache base>/agentcli/<tool>/datasets/         # datasets
```

*Why `agentcli/`:* it groups every tool in this family in one place, and keeps a tool clear of the
directory the upstream vendor's own CLI may use.

- The config file at this location is loaded automatically when present. `--config` or `<TOOL>_CONFIG`
  points to a different file; a file named that way that cannot be read is `config`.
- An XDG variable is honoured only when it is an absolute path.
- A leading `~` in any configured path is expanded to the home directory on every platform.
- If a base directory cannot be determined, for example when `HOME` is unset, the tool fails with
  `config`, and its hint lists `--config` and `--dataset-dir` along with the matching environment
  variables. It MUST NOT guess or fall back to a temporary directory.

### §13.2 Precedence

Each setting resolves independently, first match winning:

| Tier | Source | `origin` in `list-config` |
|---|---|---|
| 1 | Command-line flag | `flag` |
| 2 | `<TOOL>_<SETTING>_<PROFILE>` | `env_profile` |
| 3 | `<TOOL>_<SETTING>` | `env_default` |
| 4 | Config file, `profiles.<profile>.<setting>` | `file_profile` |
| 5 | Config file, `default.<setting>` | `file_default` |
| 6 | `AGENTCLI_<SETTING>` | `family` |
| 7 | Built-in default | `builtin` |

Two consequences matter:

- **A profile overrides only what it defines.** A setting the profile leaves out takes the value from
  the next tier down. Selecting a profile never resets a setting to its built-in default.
- **The environment beats the file.** A deployment can override a config file it does not own.

`AGENTCLI_<SETTING>` lets one variable configure every tool in the family at once.

### §13.3 Environment variable names

The profile goes last: `<TOOL>_<SETTING>_<PROFILE>`. The profile name is uppercased, with `-` replaced
by `_`, so `--profile eu-west` reads `TRELLO_TIMEOUT_EU_WEST`. *Why last:* settings form a closed
set, so matching the longest known setting name leaves the profile as the remainder, with no
ambiguity.

A profile MUST NOT be named `FILE`, since `TRELLO_API_KEY_FILE` would then be ambiguous, nor
`default`, which `list-profiles` uses for running without a profile (§7.3). Both are matched
without regard to case.

A profile exists when the config file declares it under `profiles`, or when at least one
`<TOOL>_<SETTING>_<PROFILE>` variable is set for it. Selecting a profile that exists in neither place
is `config`, and its hint SHOULD point to `list-profiles`. *Why:* a mistyped profile name would
otherwise silently fall back to the defaults.

The active profile is chosen by `--profile` or `<TOOL>_PROFILE`, and the flag wins.

### §13.4 The config file

```json
{
  "config_version": 1,
  "default": {
    "limit": 25,
    "timeout": "30s",
    "env_file": "~/.secrets/trello.env"
  },
  "profiles": {
    "work": {
      "dataset_ttl": "4h",
      "api_key_file": "~/.secrets/trello-work.key"
    },
    "readonly-demo": {
      "base_url": "http://127.0.0.1:8080"
    }
  }
}
```

| Member | Rule |
|---|---|
| `config_version` | The file format version. An unknown value is `config` |
| `default` | Settings that apply unless a profile overrides them |
| `profiles` | Named partial overrides of `default` |

Rules:

1. The file MUST be JSON. Member names are lowercase setting names.
2. An unrecognised member is `config`. *Why:* a misspelled setting that is silently ignored looks
   exactly like a setting that does not work.
3. **The file MUST NOT contain a credential value.** A credential member holding a value, rather than
   a path, is `config` at startup. Credentials are referenced only by path: `<name>_file` or
   `env_file`. *Why:* the file can then be read, shared, or committed without leaking anything —
   enforced by the tool, not left to discipline.
4. The tool never writes or creates the file.
5. `config`, `profile`, and `config_version` cannot be set from inside the file.

### §13.5 Settings

Every tool supports these, plus its own:

| Setting | Type | Default | Flag |
|---|---|---|---|
| `CONFIG` | path | platform default (§13.1) | `--config` |
| `PROFILE` | name | none | `--profile` |
| `LIMIT` | integer | 25 | `--limit` |
| `MAX_BYTES` | integer | 32768 | `--max-bytes` |
| `MAX_STRING` | integer | 2048 | `--max-string` |
| `MAX_DEPTH` | integer | 8 | `--max-depth` |
| `MAX_PAGES` | integer | 10 | `--max-pages` |
| `TIMEOUT` | duration | 30s | `--timeout` |
| `BUDGET` | duration | 60s, or 600s with `--all` | `--budget` |
| `DATASET_DIR` | path | platform default (§13.1) | `--dataset-dir` |
| `DATASET_TTL` | duration | 1h | — |
| `DATASET_MAX_BYTES` | size | 256MB | — |
| `DATASET_MAX_RECORDS` | integer | 1000000 | — |
| `DATASET_TOTAL_BYTES` | size | 2GB | — |
| `LOG_LEVEL` | `off` `error` `warn` `info` `debug` | `error` | `--verbose` sets `debug` |

Durations and sizes carry units (`30s`, `4h`, `256MB`). A bare number is `config`.

## §14 Determinism

Two identical calls against identical upstream data MUST produce byte-identical stdout. This applies
to envelopes; `--human` and `teach` output are exempt.

1. Keys appear in a stable order: the `--fields` order when given, otherwise the command's declared
   order.
2. Lists follow the command's declared `sort`, with a final tiebreak that makes the order total.
   Upstream order is never relied on.
3. Numbers from upstream are passed through as written, never converted through floating point.
4. Nothing time- or request-specific appears unless `--timing` is given (§3.2).
5. `--pretty` indents with two spaces and changes nothing else.
6. With `--deterministic`, a dataset `id` is derived from the command and its resolved parameters
   instead of generated randomly, `meta.dataset.path` is relative to the dataset directory, and any
   randomness in retry timing is turned off. The derived `id` is a name, not a cache key: a repeated
   call still fetches fresh data.

## §15 Human-readable output

`--human` is accepted by every command. It replaces the envelope with text a person can read: tables
for lists, labelled lines for objects, status words for `doctor`.

1. JSON remains the default everywhere. `--human` is never switched on automatically.
2. The exit code is identical with and without `--human`.
3. On failure, the output MUST show the error code and message.
4. It MUST show nothing the envelope would hide: secrets stay redacted and credentials stay unshown.
5. It MUST NOT use terminal escape codes. Status is shown in words (`pass`, `warn`, `FAIL`), not colour.
6. It is exempt from §14. It still respects `--limit`, `--fields`, and the size caps.
7. `--human` with `--pretty` is a `usage` error.

## §16 MCP server mode

### §16.1 Availability

Server mode runs the tool as a long-lived Model Context Protocol server. Whether a given tool ships
with it is that tool's decision.

Server mode is an additional way to run the same binary, entered only through `serve`. A tool built
with server mode MUST meet every other section of this contract when run as a command-line tool,
exactly as a build without it does. *Why:* agents and operators rely on the command line whether or
not a server is also available, and a build that works only as a server is not a conforming tool.

- A tool built with server mode MUST support both transports: stdio and Streamable HTTP.
- A tool built without it MUST report `mcp_enabled:false` in `describe` and `version`, and MUST
  answer `serve` with `refused` and `details.reason:"mcp_disabled"`, not with `usage`. *Why:* "not
  built" and "not a command" call for different responses from the caller.
- Every tool, with or without server mode, MUST publish its commands in MCP tool shape through
  `describe` (§6.2), so turning server mode on requires no redesign.

```
trello serve                                      # stdio
trello serve --transport http                     # Streamable HTTP on 127.0.0.1:7810
trello serve --transport http --addr 0.0.0.0:7810 --allow-remote \
             --tls-cert-file /etc/trello/tls/cert.pem --tls-key-file /etc/trello/tls/key.pem
```

`serve` accepts only flags that configure the server itself: transport, address, `--allow-remote`,
`--token-file`, `--tls-cert-file`, `--tls-key-file`, `--tls-terminated-upstream`, `--profile`,
`--config`, `--verbose`, and `--deterministic`. Per-call flags such as
`--fields` or `--limit` arrive as tool arguments instead.

### §16.2 Tools over MCP

- The server's tool list comes from the same definitions as `describe`, and names, parameters, and
  types MUST match it exactly. Each tool's `outputSchema` is the command's full output schema (§6.2). A read-only build lists no mutating tools.
- `teach` is offered as a tool. `tools` and `describe` are not; the protocol's own tool listing
  replaces them. `list-profiles` is not offered either: a server's profile is fixed when it starts
  (§16.1), so a client has no use for the list.
- Every per-call flag in §17 is accepted as a tool argument, named as the flag without its leading
  dashes and with `-` replaced by `_` (`--max-bytes` becomes `max_bytes`).
- A tool call returns the same envelope the equivalent command line would, both as structured content
  and as one text block, with `isError` equal to `!ok`. `error.exit_code` stays in the envelope, so §4
  still applies without a process exit.
- A mutating tool takes a `confirm` boolean and a `dry_run` boolean. Without `confirm` it behaves as
  §11.2: refused, with a preview, and nothing sent.
- `initialize` and the tool list MUST work with no configuration and no credential.
- Timeouts and budgets apply per call, not to the server's lifetime.
- Datasets created during a session remain readable after it ends, until they expire.
- On startup and shutdown the server MAY write one line each to stderr, stating the transport,
  address, whether writes are enabled, and whether HTTP authentication is on. Neither line may
  contain a secret.

*Why read-only builds matter here:* agent permission rules written for the command line never see an
MCP call. Whatever the client allows goes through, so a read-only build is the only write protection
that survives the change of transport.

### §16.3 Over HTTP

Serving over HTTP makes the tool a network service that holds an upstream credential and uses it for
whoever connects. These rules follow from that.

**A remote listener never runs in plaintext by default.** A bearer token sent over unencrypted HTTP
can be read by anyone on the network path and reused, which would make authentication worthless. Any
non-loopback address therefore requires, in addition to `--allow-remote` and a token, exactly one of:

| Option | Meaning |
|---|---|
| `--tls-cert-file <path>` and `--tls-key-file <path>` | The tool serves HTTPS itself, at TLS 1.2 or later. Self-issued certificates are acceptable; clients must trust the issuing CA |
| `--tls-terminated-upstream` | The operator declares that something in front of the tool — a reverse proxy, ingress, service mesh, or encrypted overlay network — terminates TLS, and that the hop to the tool is private. The tool cannot verify this; the flag records it as a deliberate decision |

With neither, a non-loopback bind is `config` and the server does not start. Both can also be set
through `<TOOL>_MCP_TLS_CERT_FILE`, `<TOOL>_MCP_TLS_KEY_FILE`, and `<TOOL>_MCP_TLS_TERMINATED_UPSTREAM`.
Giving both a certificate and `--tls-terminated-upstream` is `usage`.

On loopback, TLS is optional: plaintext there never crosses a network.

| Area | Rule |
|---|---|
| Address | Default `127.0.0.1:7810`. A bare port binds to loopback, never to all interfaces |
| Remote binding | Any address that is not loopback — `0.0.0.0` and `::` count as remote — requires `--allow-remote`, a configured token, and TLS as above. Otherwise `config` |
| Token source | `<TOOL>_MCP_TOKEN`, then `<TOOL>_MCP_TOKEN_FILE`, then `--token-file <path>`. Never the token itself on the command line. Token files follow §12.4 |
| Authentication | Required off loopback, optional on it. When set, every request except the health check must carry it; compared in constant time; failures answered `401` with no detail |
| Origin | A request with a non-loopback `Origin` header is answered `403`. A request with no `Origin` is allowed. *Why:* this blocks web pages from reaching a loopback server through DNS rebinding |
| TLS | The certificate and key are read from files; the key file follows §12.4. The startup line MUST say which TLS option is in effect, and with `--tls-terminated-upstream` MUST state that the tool itself is serving plaintext |
| Request size | Bodies over 1 MiB are answered `413` |
| Sessions | Follow the MCP specification for session ids and protocol version headers |
| Health | `GET /health`, unauthenticated, returns only tool name, versions, `writes_enabled`, and `mcp_enabled`. Never addresses, paths, credential details, or the command list |
| Writes | When writes are enabled and the address is not loopback, the tool MUST print a warning recommending a read-only build |
| Datasets | `meta.dataset.path` is omitted, since the client may not share the filesystem. `dataset.read`, `dataset.list`, and `dataset.stat` are offered; `dataset.clear` is not |

## §17 Reserved names

These command names are reserved, and a tool MUST NOT use them as a group name: `tools`, `describe`,
`teach`, `doctor`, `list-config`, `list-profiles`, `dataset`, `serve`, `version`, `write`, `help`.
`write` may appear only as the optional first segment of a mutating command (§11.1).

These flags are reserved. Every tool MUST implement those that apply to it with exactly this meaning,
and MUST NOT use any of them for anything else:

| Flag | Meaning | Section |
|---|---|---|
| `--pretty` | Indented JSON | §14 |
| `--human` | Human-readable output | §15 |
| `--fields` | Field projection | §8.2 |
| `--keep-empty` | Keep empty values | §8.3 |
| `--limit` | Records to return | §8.1 |
| `--cursor` | Continue from a cursor | §9.3 |
| `--max-bytes`, `--max-string`, `--max-depth`, `--max-pages` | Size caps | §8.1 |
| `--since`, `--until` | Time window | §8.5 |
| `--all` | Collect a dataset | §10.1 |
| `--offset` | Position within a dataset | §10.3 |
| `--confirm` | Allow a write | §11.2 |
| `--dry-run` | Preview without sending | §11.2 |
| `--profile` | Select a profile | §13.3 |
| `--config` | Config file path | §13.1 |
| `--dataset-dir` | Dataset directory | §13.1 |
| `--timeout`, `--budget` | Per-request and per-invocation deadlines | §13.5 |
| `--timing` | Add timing to `meta` | §3.2 |
| `--deterministic` | Deterministic ids and paths | §14 |
| `--verbose` | Debug diagnostics on stderr | §2 |
| `--schema` | Config schema, on `list-config` | §7.2 |
| `--detail` | Tier 2, on `tools` | §6.1 |
| `--list` | Topic index, on `teach` | §6.3 |
| `--transport`, `--addr`, `--allow-remote`, `--token-file` | Server options, on `serve` | §16 |
| `--tls-cert-file`, `--tls-key-file`, `--tls-terminated-upstream` | Server TLS, on `serve` | §16.3 |
| `--version` | Same as the `version` command | §18 |
| `--help`, `-h` | Help text | §6.4 |

The credential flags listed in §12.2 are forbidden.

No command returning upstream data may offer a table, CSV, or plain-text output mode other than
`--human`.

## §18 Versioning and change control

`version` (or `--version`) returns an envelope whose `data` holds `tool`, `tool_version`,
`contract_version`, `writes_enabled`, `mcp_enabled`, `os`, `arch`, and the build commit.

The contract version is `major.minor`:

- A change that only adds something bumps the minor version.
- A change to the meaning of an exit code, the meaning of a flag, or the shape of the envelope bumps
  the major version.
- A removed flag stays accepted for one further minor version, with a warning naming its
  replacement.
- The meaning of a published exit code never changes.
- The MCP protocol revision a tool speaks is negotiated per session and is separate from this
  version. Adding a newer revision is a minor change; dropping one a released tool accepted is a
  major change.

This document holds no record of its own past. Every change is recorded as a new file under
`history/`, in the same commit as the change. A history file is never edited once committed; a
correction is a new file naming the one it corrects.

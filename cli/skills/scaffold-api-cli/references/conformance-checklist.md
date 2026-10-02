# Conformance Checklist

Checks derived from `../../../contract/CONTRACT.md`. Each ID names the contract section it tests:
`C8.4-2` is the second check for §8.4. When a section changes, revisit the checks with its number.
The contract is authoritative; if this list and the contract disagree, fix this list. IDs are never
reused or renumbered, so tests and notes that cite one stay valid; a new check takes the next free
number in its section.

Last regenerated from contract 1.4.2: every MUST and MUST NOT has a check, and a SHOULD with a
measurable budget has one reported as a warning.

`../scripts/verify_conformance.sh` runs the items marked **auto** against a built binary, with
nothing configured and no upstream. Items marked **manual** need a fake upstream that counts
requests, a controllable clock, a second build flavour, or an MCP client; implement those in the
tool's own test suite.

`$T` is the binary and `$P` its environment prefix.

## §2 Output streams

- **C2-1** auto — Every command outside the exceptions writes exactly one JSON document and one
  trailing newline.
- **C2-2** auto — An error envelope appears on stdout, not stderr.
- **C2-3** auto — No stream contains an ANSI escape byte (`0x1b`).
- **C2-4** auto — Stdout is identical with and without a terminal attached. (Skipped when no pty can
  be allocated.)
- **C2-5** auto — No command blocks on stdin.
- **C2-6** manual — Killing the process mid-write leaves either nothing or a complete document when
  stdout is a file, or a pipe and the document fits its buffer (the default `--max-bytes` does).
  SIGINT and SIGTERM during a blocked pipe write still deliver the complete document.

## §3 The envelope

- **C3-1** auto — A success envelope has exactly `ok`, `tool`, `command`, `data`, `meta` at the top
  level.
- **C3-2** auto — `meta.contract_version` matches `major.minor[.patch]`, and `meta.tool_version` is present.
- **C3-3** auto — Without `--timing`, `meta` contains no `timing` member.
- **C3-4** auto — An error envelope has `error.code`, `error.exit_code`, `error.message`,
  `error.retriable`; `exit_code` equals the process exit status; `data` is `null` (except `partial`,
  which keeps the records retrieved, and `doctor`, which keeps its checks).
- **C3-5** auto — `error.message` is at most 512 characters and `error.details` at most 2048 bytes.
- **C3-6** auto — A usage error prints no help text: stdout is one JSON document.
- **C3-7** auto — An unknown command's error names it in `details`.
- **C3-8** manual — An empty result is `ok:true`, `data:[]`, `meta.page.count:0`, exit 0.
- **C3-9** manual — `command` is `null` only when the arguments named no recognisable command,
  including in `canceled` and `internal` envelopes.
- **C3-10** manual — A `usage` error for an unknown flag names it in `details`, with `did_you_mean`
  when a close match exists.
- **C3-11** manual — Without `--timing`, no envelope contains a wall-clock value, duration, request
  identifier, or hostname.
- **C3-12** manual — Upstream text copied into `details` is at most 512 characters and redacted.

## §4 Error codes and exit codes

- **C4-1** auto — Every error produced during the run pairs code and exit exactly as the contract's
  table does.
- **C4-2** auto — Every tool-specific code in `describe.exit_codes` is between 32 and 63.
- **C4-3** manual — SIGINT produces a `canceled` envelope and exit 130; SIGTERM exit 143.
- **C4-4** manual — A recovered crash produces an `internal` envelope and exit 1.

## §5 Command naming

- **C5-1** auto — Every name in `tools` has lowercase dotted segments; domain commands have at least
  two.
- **C5-2** auto — Every `describe` entry's `argv` equals its name split on dots.
- **C5-3** auto — `readOnlyHint` in `describe` is the opposite of `mutates` in `tools --detail` for
  every command, and every name starting with `write.` is mutating.
- **C5-4** auto — No domain command's first segment is a reserved name.

## §6 Discovery

- **C6-1** auto — `tools`, `tools --detail`, `describe`, `teach`, `list-config`, and `list-profiles`
  exit 0 with nothing configured.
- **C6-2** auto — `tools` returns a sorted, flat array of strings.
- **C6-3** auto — `tools --detail` entries have exactly `name`, `description`, `mutates`; each
  description is one line of at most 160 characters.
- **C6-4** auto — `describe <name>` returns exactly one command; an unknown name is `usage`.
- **C6-5** auto — Every `describe` entry has `argv`, `inputSchema`, `outputSchema`,
  `annotations.readOnlyHint`, and `x-cli`.
- **C6-6** auto — `envelope_schema` appears exactly once in `describe`.
- **C6-7** auto — Discovery output is complete under `--max-bytes 1`.
- **C6-8** auto — Bare `teach` is Markdown, exits 0, and names `teach contract`.
- **C6-9** auto — Bare `teach` is under 4096 bytes. (Reported as a warning; the limit is a SHOULD.)
- **C6-10** auto — `teach contract`, `teach flags`, `teach config`, and `teach --list` exit 0 and
  return Markdown.
- **C6-11** auto — `teach config` names every setting `list-config` reports.
- **C6-12** auto — Every dotted command name in any `teach` output exists in `tools`.
- **C6-13** auto — An unknown `teach` topic is `usage`.
- **C6-14** auto — `--help` exits 0 and its last line is `# machine-readable: <tool> describe …`.
- **C6-15** manual — Bare `teach` does not list the full command inventory.
- **C6-16** manual — `teach flags <name>` answers for every reserved flag the build supports.
- **C6-17** auto — No `outputSchema` in `describe` has, at any depth, a `required` array,
  `additionalProperties:false`, or a string schema with `maxLength`, `pattern`, `enum`, or `const`.
- **C6-18** manual — In every `outputSchema`, each object or array schema nested inside a record also
  admits `"<depth-elided>"`. A response under `--max-depth 1` validates against the command's full
  output schema, and so does a response projected with `--fields`.
- **C6-19** auto — `tools --detail` is under 8192 bytes. (Reported as a warning; the limit is a
  SHOULD.)
- **C6-20** manual — `teach contract` is the same text for every tool in the family: the bundle's
  shared template, rendered.
- **C6-21** manual — `teach config <aspect>` opens each aspect alone. `teach config` gives every
  setting's default, environment variable, and flag; names credentials by path, never by value; and
  includes an example file with a profile that sets more than one value. In a tool that supports
  profile descriptions, the profiles aspect also explains `description`. In a tool with a
  profile-scoped credential (C12-8), the credentials aspect says which credentials are profile-scoped
  and what that means.

## §7 Diagnostics

- **C7-1** auto — Every `doctor` check has `status` of `pass`, `warn`, `fail`, or `skip`, and the
  required checks appear in the contract's order.
- **C7-2** auto — With no credentials, `doctor` fails the `credentials` check, exits non-zero, keeps
  `data.checks`, and reports `auth` as `skip`.
- **C7-3** manual — `doctor` issues no mutating upstream request.
- **C7-4** auto — `list-config` reports every contract setting, each with `name`, `value`, `source`,
  `origin`, and `default`.
- **C7-5** auto — An unset setting reports `origin:"builtin"` and `source:"builtin"`.
- **C7-6** auto — A credential reports `value:null` and a boolean `set`.
- **C7-7** auto — `list-config --schema` returns a schema object, and no credential can be given a
  value under it.
- **C7-8** manual — A `doctor` check that cannot run because an earlier one failed is reported as
  `skip`, never omitted, and `doctor` exits 0 exactly when no check is `fail`.
- **C7-9** auto — `list-profiles` returns `[]` when no profile is declared.
- **C7-10** auto — With profiles declared in the config file and by variables, `list-profiles` lists
  `default` first (active, `declared_in:[]`), then each profile by name with its `declared_in`; an
  environment-only profile appears under its lowercase segment; entries have exactly `name`,
  `active`, `declared_in`, plus `description` in a tool that supports descriptions (C7-13).
- **C7-11** auto — The profile selected by `--profile` is the only active entry.
- **C7-12** auto — An undeclared selected profile gives exit 0, no active entry, and a warning.
- **C7-13** auto, conditional — Applies only to a tool that supports profile descriptions; skipped
  only when a config file with a described profile is rejected as an unrecognised member (exit 3,
  `config`, message or hint names `description` as unrecognised); any other failure on that file is a FAIL. `description` is
  present on every `list-profiles` entry (`default` included): the file's string for a described
  profile, `null` for an undescribed one and for an environment-only one. It is never omitted.
- **C7-14** auto, conditional — Same condition. `description` does not appear among `list-config`
  settings.
- **C7-15** auto, conditional — Applies only when `describe` marks a credential `profile_scoped:true`;
  otherwise skipped. With a profile selected and only the default scope holding a value, the
  `list-config` entry for that credential is `set:false`, `value:null`, `source` and `origin`
  `builtin`, and its `hint` names the skipped default-scope variable.
- **C7-16** auto, conditional — Same condition. In that situation `doctor` fails the `credentials`
  check and reports `auth` as `skip`.

## §8 Context discipline

- **C8.1-1** manual — A list command with no flags returns at most the default `--limit`.
- **C8.1-2** manual — `--limit` above the maximum is clamped with a `limit_clamped` warning, exit 0.
- **C8.2-1** manual — `--fields a,b` returns exactly those keys in that order.
- **C8.2-2** manual — `-field` removes from the default set; mixing forms is `usage`.
- **C8.2-3** manual — An unavailable path is `usage` with `did_you_mean`.
- **C8.3-1** manual — Empty values are removed, `false` and `0` kept, `meta.stripped_empty:true`.
- **C8.4-1** manual — Over `--max-bytes`, whole records are dropped from the end; `truncated`,
  `truncated_reason`, `dropped`, and `next_cursor` are set; exit 0; the output is valid JSON.
- **C8.4-2** manual — A string over `--max-string` is shortened and listed in `meta.elided_fields`.
- **C8.4-3** manual — A single object over `--max-bytes` is kept, its largest strings shortened and
  listed in `meta.elided_fields`; `data` is never `null` because of size.
- **C8.4-4** manual — A subtree deeper than `--max-depth` is replaced by `"<depth-elided>"`.
- **C8.4-5** manual — When the envelope alone exceeds `--max-bytes`, the cap is ignored for it and
  `truncated_reason` is `"max_bytes_below_minimum"`.
- **C8.4-6** manual — When not even one record fits, the response carries the first record with its
  largest strings shortened and listed in `meta.elided_fields`, and `next_cursor` points at the next
  record. Following cursors under a tiny `--max-bytes` reaches the end of the listing.
- **C8.5-1** manual — A time-windowed command defaults to the last 24 hours and reports
  `meta.window` with `source:"default"`.
- **C8.5-2** manual — `2h`, `+2h`, and an offset-less date-time are `usage`.
- **C8.5-3** manual — `--since` not earlier than `--until` is `usage`; every time in output is RFC 3339
  UTC with a `Z` suffix.

## §9 Paging

- **C9-1** manual — `--limit 60` against 20-per-page upstream returns 60 records from 3 requests.
- **C9-2** manual — Page requests to upstream never overlap in time.
- **C9-3** manual — `meta.page` has the same members on every list response.
- **C9-4** manual — `next_cursor` is `null` exactly when `has_more` is `false`.
- **C9-5** manual — The cursor alone continues the listing with the original parameters.
- **C9-6** manual — A cursor passed to a different command is `usage`.

## §10 Datasets

- **C10-1** manual — `--all` writes a JSON Lines file at `meta.dataset.path`, even for a small set.
- **C10-2** manual — Stored records match `--fields` and empty-stripping.
- **C10-3** manual — Reading a dataset to the end issues no upstream request.
- **C10-4** manual — A collection cap gives `complete:false` and exit 0; an upstream failure midway
  gives `partial`, exit 13, with the collected records kept.
- **C10-5** manual — An unwritable dataset directory is `config`, exit 3, with no fallback.
- **C10-6** auto — `dataset read` with an unknown id is `cache_miss`, exit 14, with a hint.
- **C10-7** manual — A dataset idle beyond `DATASET_TTL` is removed on a later invocation (injected
  clock).
- **C10-8** manual — Over `DATASET_TOTAL_BYTES`, the least recently read datasets are removed first.
- **C10-9** manual — Dataset directory and files are owner-only.
- **C10-10** manual — `dataset clear` without `--confirm` is `refused`.
- **C10-11** manual — The `--all` response's `meta.page.next_cursor` is a cursor for `dataset read`,
  and `meta.dataset.read_hint` runs as-is.
- **C10-12** manual — Each read records its time in the sidecar and extends the dataset's lifetime;
  filesystem access times are not used. A cleanup failure is a warning, never a failed command.

## §11 Writes

- **C11-1** auto — Every mutating command in `tools --detail`, run without `--confirm` and with dummy
  credentials, is not sent. Each outcome is either exit 8 `refused` or exit 2 `usage` (no request
  could be built, so nothing was sent). Dummy values follow each required parameter's `inputSchema`
  type: `1` for integer and number, the bare flag for boolean, `x` for anything else; positional
  parameters follow `x-cli.positional`. Any other exit is a FAIL naming the command and exit code,
  including 0, network and upstream codes, and 3 or 4: a confirm check placed after credential
  handling is suspicious, because a tool with no credential must still refuse an unconfirmed write.
  A refusal without `details.preview` counts only when it carries a `details.reason` that is not
  about confirmation (`chat_not_allowed`, say); that is "not sent", not the preview proof. At least
  one command must be `refused` with `details.preview` an object; if every command gives `usage` or
  another refusal, the check FAILs ("no write command could be exercised"), so give at least one
  write a dummy-satisfiable argument set. (Skipped when the build has no writes.)
- **C11-2** manual — That refusal, and `--dry-run`, send zero requests upstream.
- **C11-3** manual — A read-only build has no mutating command in `tools`, `describe`, `teach`, or the
  MCP tool list; refuses a command line naming one with `writes_disabled`; and reports
  `writes_enabled:false` in `describe`, `doctor`, `version`, and the server's health endpoint.
- **C11-4** manual — A timed-out write is not retried and reports `write_state:"unknown"`.
- **C11-5** manual — Every command accepts `--dry-run`: nothing mutating is sent, `meta.dry_run` is
  `true`, and a command that would send a request returns it in `data.preview`. `--dry-run` with
  `--confirm` is still a dry run.

## §12 Secrets

- **C12-1** auto — `--api-key <v>` and `--token=<v>` are `refused` with `secret_on_argv`, exit 8, and
  `<v>` appears nowhere in the output. (Manual addition: the message tells the operator to rotate
  the credential.)
- **C12-2** auto — A credential value set in the environment never appears in the output of
  `tools`, `describe`, `teach`, `list-config`, `list-profiles`, or `doctor`.
- **C12-3** auto — A credential file with group or other permission bits is `config`, exit 3.
  (Skipped on platforms without POSIX modes.)
- **C12-4** manual — An upstream response echoing the credential, raw, URL-encoded, or base64, never
  reaches stdout, stderr, or a dataset.
- **C12-5** manual — Within a scope, the direct variable wins over the `_FILE` variable; a
  profile-scoped variable wins over an unscoped one.
- **C12-6** manual — In `--verbose` logs, query parameters whose names suggest secrets are masked,
  and headers appear by name only, except the fixed list of harmless headers.
- **C12-7** manual — Where POSIX permissions are unavailable, a credential file is not refused: a
  warning names it, and `doctor` reports its permissions as `unverified`.
- **C12-8** auto, conditional — Applies only when `describe` marks a credential `profile_scoped:true`
  (since 1.4.2); otherwise skipped, as for a tool that does not use the feature. With a profile
  selected whose own section names a `<name>_file`, while the unsuffixed variable (and an unsuffixed
  `_FILE` variable naming a missing file) is set, `list-config` shows the credential `set:true`,
  `origin:"file_profile"`: the profile's own source won, and the default-scope file was not opened.
- **C12-9** auto, conditional — Same condition. A selected profile with nothing in its own scope while
  the unsuffixed variable is set: `doctor` exits 3 with `config`, `details.profile`, and
  `details.would_use_source` equal to the skipped variable's name, never a value; a `hint` is
  present and the value appears nowhere in the output.
- **C12-10** auto, conditional — Same condition. A selected profile with a credential in neither
  scope: `doctor` exits 4 with `auth`.
- **C12-11** auto, conditional — Same condition. With no profile selected, the unsuffixed variable is
  used: `set:true`, `origin:"env_default"`.
- **C12-12** manual — A profile-suffixed key in the `env_file` of the `default` section counts as the
  profile's own source; an unsuffixed key there, and `default.<name>_file`, do not. Settings other than
  the profile-scoped credential still inherit from `default`.

## §13 Configuration

- **C13-1** auto — A config file at the platform default location is loaded automatically.
- **C13-2** auto — A file named by `--config` that does not exist is `config`, exit 3.
- **C13-3** auto — An unparseable config file is `config`, exit 3.
- **C13-4** auto — An unrecognised member is `config`, exit 3.
- **C13-5** auto — A credential value in the file is `config`, exit 3, and the value is not echoed.
- **C13-6** auto — A setting only in `default` has `origin:"file_default"`.
- **C13-7** auto — With `--profile F`, a setting in `profiles.F` has `origin:"file_profile"`.
- **C13-8** auto — The same setting in the file and in `$P_S` resolves to `env_default`.
- **C13-9** auto — `$P_S_F` with `--profile F` resolves to `env_profile`.
- **C13-10** auto — With a profile active, a setting only in `$P_S` still resolves to `env_default`,
  not `builtin`.
- **C13-11** auto — `AGENTCLI_S` resolves to `family` when nothing higher is set.
- **C13-12** auto — `$P_PROFILE` selects a profile, and `--profile` overrides it.
- **C13-13** auto — An unknown profile is `config`, exit 3.
- **C13-14** manual — With no determinable home directory, the tool fails with `config` rather than
  guessing.
- **C13-15** manual — The tool never writes the config file.
- **C13-16** auto — Profiles named `FILE` and `default` are rejected.
- **C13-17** auto — A duration or size without a unit is `config`, exit 3.
- **C13-18** auto — The hint for an undeclared profile points to `list-profiles`. (Reported as a
  warning; the hint is a SHOULD.)
- **C13-19** auto, conditional — Applies only to a tool that supports profile descriptions. A
  `description` of 201 characters, one containing a line break, a non-string one, and a blank one are
  each `config`, exit 3.

## §14 Determinism

- **C14-1** auto — Two identical `describe` calls produce identical bytes.
- **C14-2** auto — `--pretty` parses to the same JSON as the compact form.
- **C14-3** manual — Two identical list calls against identical upstream data produce identical bytes.
- **C14-4** manual — `--deterministic` gives a dataset id derived from the request, and a relative
  path.
- **C14-5** manual — Numbers from upstream are passed through as written: large integers and long
  decimals are unchanged.
- **C14-6** manual — Lists follow the declared `sort` with a total tiebreak, whatever order upstream
  returns.

## §15 Human-readable output

- **C15-1** auto — `--human` on `tools` produces output that is not JSON, and exits 0.
- **C15-2** auto — `--human` on an unknown command exits 2 and shows the error code.
- **C15-3** auto — `--human` with `--pretty` is `usage`, exit 2.
- **C15-4** manual — `--human` never shows a credential or anything the envelope omits.

## §16 MCP server mode

- **C16-1** auto — `describe.mcp_enabled` is a boolean. When `false`, `serve` is `refused` with
  `mcp_disabled`, exit 8.
- **C16-2** manual — When enabled, both stdio and Streamable HTTP work.
- **C16-3** manual — The server's tool list equals `describe`, name for name and parameter for
  parameter, offers `teach`, and omits `tools`, `describe`, and `list-profiles`.
- **C16-4** manual — A tool result carries the same envelope as the command line, with `isError`
  equal to `!ok`.
- **C16-5** manual — Over HTTP: loopback by default; a non-loopback `Origin` gets 403; a body over
  1 MiB gets 413; `/health` reveals nothing sensitive.
- **C16-6** manual — A remote bind without `--allow-remote`, without a token, or without a TLS decision
  (certificate and key, or `--tls-terminated-upstream`) fails with `config` and never listens.
- **C16-7** manual — With a certificate and key, the listener serves only TLS 1.2 or later; a key file
  readable by others is `config`; giving both a certificate and `--tls-terminated-upstream` is `usage`.
- **C16-8** manual — A build with server mode passes the full automated suite
  (`verify_conformance.sh`) as a command-line tool, with the same results as a build without it.
- **C16-9** manual — Each tool's `outputSchema` is the envelope schema with `data` replaced by
  `{"anyOf":[<outputSchema>,{"type":"null"}]}`; successful and failed results both validate against it.
- **C16-10** manual — Every per-call flag is accepted as an argument named without dashes and with
  `-` as `_`. A mutating tool takes `confirm` and `dry_run`; without `confirm` it is refused with a
  preview and sends nothing.
- **C16-11** manual — `initialize` and the tool list work with no configuration and no credential.
- **C16-12** manual — Over HTTP, a token is required off loopback; a missing or wrong token gets 401
  with no detail. The startup line names the transport, address, whether writes are enabled, whether
  authentication is on, and the TLS option, says the tool serves plaintext under
  `--tls-terminated-upstream`, and contains no secret. With writes enabled on a non-loopback address,
  a warning recommends a read-only build.
- **C16-13** manual — Over HTTP, `meta.dataset.path` is omitted and `dataset.clear` is not offered.
  Datasets created during a session stay readable after it ends, until they expire.

## §17 Reserved names

- **C17-1** auto — No command's parameter flag is a reserved flag (§17) or a credential flag (§12.2).

## §18 Versioning

- **C18-1** auto — `version` and `--version` return the same envelope, with `tool`, `tool_version`,
  `contract_version`, `writes_enabled`, `mcp_enabled`, `os`, and `arch`.

## Highest-stakes failures

Treat these as blocking above the rest:

- **C12-1 to C12-4 and C13-5** — a credential can escape, which is the reason this family exists.
- **C11-1 and C11-2** — a write can happen without confirmation.
- **C16-6** — a remote server can start in plaintext by default, exposing its bearer token to the
  network.
- **C13-10** — profiles silently discard lower-tier settings, which works in testing and misbehaves
  wherever profiles are actually used.
- **C10-3** — reading a dataset quietly re-queries upstream, costing rate limit and possibly
  returning shifted data.

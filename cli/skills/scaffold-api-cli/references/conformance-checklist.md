# Conformance Checklist

Checks derived from `../../../contract/CONTRACT.md`. Each ID names the contract section it tests:
`C8.4-2` is the second check for §8.4. When a section changes, revisit the checks with its number.
The contract is authoritative; if this list and the contract disagree, fix this list.

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
- **C2-6** manual — Killing the process mid-write leaves either nothing or a complete document.

## §3 The envelope

- **C3-1** auto — A success envelope has exactly `ok`, `tool`, `command`, `data`, `meta` at the top
  level.
- **C3-2** auto — `meta.contract_version` matches `major.minor`, and `meta.tool_version` is present.
- **C3-3** auto — Without `--timing`, `meta` contains no `timing` member.
- **C3-4** auto — An error envelope has `error.code`, `error.exit_code`, `error.message`,
  `error.retriable`; `exit_code` equals the process exit status; `data` is `null`.
- **C3-5** auto — `error.message` is at most 512 characters and `error.details` at most 2048 bytes.
- **C3-6** auto — A usage error prints no help text: stdout is one JSON document.
- **C3-7** auto — An unknown command's error names it in `details`.
- **C3-8** manual — An empty result is `ok:true`, `data:[]`, `meta.page.count:0`, exit 0.

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
- **C5-3** auto — `mutates` in `tools --detail` and `readOnlyHint` in `describe` both agree with
  whether the name starts with `write.`.
- **C5-4** auto — No domain command's first segment is a reserved name.

## §6 Discovery

- **C6-1** auto — `tools`, `tools --detail`, `describe`, `teach`, and `list-config` exit 0 with
  nothing configured.
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
- **C8.5-1** manual — A time-windowed command defaults to the last 24 hours and reports
  `meta.window` with `source:"default"`.
- **C8.5-2** manual — `2h`, `+2h`, and an offset-less date-time are `usage`.

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

## §11 Writes

- **C11-1** auto — A `write.*` command without `--confirm` is `refused`, exit 8, with
  `details.preview`. (Skipped when the build has no writes.)
- **C11-2** manual — That refusal, and `--dry-run`, send zero requests upstream.
- **C11-3** manual — A read-only build has no `write.*` in `tools`, `describe`, `teach`, or the MCP
  tool list, and refuses a `write` command line with `writes_disabled`.
- **C11-4** manual — A timed-out write is not retried and reports `write_state:"unknown"`.

## §12 Secrets

- **C12-1** auto — `--api-key <v>` and `--token=<v>` are `refused` with `secret_on_argv`, exit 8, and
  `<v>` appears nowhere in the output.
- **C12-2** auto — A credential value set in the environment never appears in the output of
  `tools`, `describe`, `teach`, `list-config`, or `doctor`.
- **C12-3** auto — A credential file with group or other permission bits is `config`, exit 3.
  (Skipped on platforms without POSIX modes.)
- **C12-4** manual — An upstream response echoing the credential, raw, URL-encoded, or base64, never
  reaches stdout, stderr, or a dataset.
- **C12-5** manual — Within a scope, the direct variable wins over the `_FILE` variable; a
  profile-scoped variable wins over an unscoped one.

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

## §14 Determinism

- **C14-1** auto — Two identical `describe` calls produce identical bytes.
- **C14-2** auto — `--pretty` parses to the same JSON as the compact form.
- **C14-3** manual — Two identical list calls against identical upstream data produce identical bytes.
- **C14-4** manual — `--deterministic` gives a dataset id derived from the request, and a relative
  path.

## §15 Human-readable output

- **C15-1** auto — `--human` on `tools` produces output that is not JSON, and exits 0.
- **C15-2** auto — `--human` on an unknown command exits 2 and shows the error code.
- **C15-3** auto — `--human` with `--pretty` is `usage`, exit 2.
- **C15-4** manual — `--human` never shows a credential or anything the envelope omits.

## §16 MCP server mode

- **C16-1** auto — `describe.mcp_enabled` is a boolean. When `false`, `serve` is `refused` with
  `mcp_disabled`, exit 8.
- **C16-2** manual — When enabled, both stdio and Streamable HTTP work.
- **C16-8** manual — A build with server mode passes the full automated suite
  (`verify_conformance.sh`) as a command-line tool, with the same results as a build without it.
- **C16-3** manual — The server's tool list equals `describe`, name for name and parameter for
  parameter, and omits `tools` and `describe`.
- **C16-4** manual — A tool result carries the same envelope as the command line, with `isError`
  equal to `!ok`.
- **C16-5** manual — Over HTTP: loopback by default; a non-loopback `Origin` gets 403; a body over
  1 MiB gets 413; `/health` reveals nothing sensitive.
- **C16-6** manual — A remote bind without `--allow-remote`, without a token, or without a TLS decision
  (certificate and key, or `--tls-terminated-upstream`) fails with `config` and never listens.
- **C16-7** manual — With a certificate and key, the listener serves only TLS 1.2 or later; a key file
  readable by others is `config`; giving both a certificate and `--tls-terminated-upstream` is `usage`.

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

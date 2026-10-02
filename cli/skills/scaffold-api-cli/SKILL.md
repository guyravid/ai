---
name: scaffold-api-cli
description: Scaffold a new contract-compliant, agent-facing CLI that wraps an external HTTP API, so an AI agent can query it by shelling out without API keys ever entering the agent's context. Generates tools, describe, tiered teach, doctor, and list-config; the shared JSON envelope; --fields projection and size caps; paging and --all datasets; write gating with --confirm; hardened secret handling; profile-aware configuration; --human output; and optional MCP server mode, leaving slots for the API-specific commands. Use this skill when the user wants a command-line wrapper around an API for agent use. Trigger on "build a CLI for X API", "wrap this API in a CLI", "agent-facing CLI", "CLI so Claude can query X", "scaffold an API CLI", "MCP server for X API", or any request to give an agent API access without exposing credentials. Not for general-purpose CLIs, for adding a command to an existing conforming CLI (edit its registry), or for TypeScript MCP servers under mcps/.
---

# Scaffold an Agent-Facing API CLI

Generate a command-line tool that gives an automated agent access to one external API, conforming to
the contract in this bundle. The tool keeps credentials in its own environment, so they never reach
the agent.

Work through the steps in order. Confirm the command surface with the user before generating
anything.

---

## Step 1 — Load the contract

Find the bundle first. This skill lives at `cli/skills/scaffold-api-cli/` inside the bundle, and the
paths below are relative to that real location. If the skill was installed through a symlink, resolve
it (`realpath` on this skill's directory) before reading; relative to the symlink the paths do not
exist.

Read these, in this order:

```
../../contract/CONTRACT.md       # what a conforming tool does (normative)
../../base/BASE_TEMPLATE.md      # how to build one
../../base/patterns/envelope.md  # schemas, examples, cursor format
../../base/patterns/go.md        # the Go realization
```

If any is missing, stop and say so. This skill has no authority of its own; it turns the contract
into generation steps and cannot produce a conforming tool without it.

`../../base/teach/orientation.md.tmpl` and `../../base/teach/shared.md.tmpl` are vendored in Step 8.

---

## Step 2 — Interrogate the API

Work through `references/interrogation-questions.md`. Do not skip or improvise the question set; a
missed answer turns into a rewrite in Step 6.

If the API publishes an OpenAPI document, fetch it and propose the command list from it. Otherwise
ask for the three to eight operations the agent actually needs. Do not wrap the whole API: eight
well-chosen commands serve an agent better than eighty.

---

## Step 3 — Decide the command surface

Map each operation using `references/tool-mapping.md`, and confirm the table with the user:

| Name | Command line | Parameters | Returns | Paged | Collectable | Time window | Writes |
|---|---|---|---|---|---|---|---|
| `cards.list` | `cards list` | `board` (required), `list` | card[] | yes | yes | no | no |
| `cards.create` | `cards create` | `list`, `name` (required) | card | no | no | no | yes |

Fixed by the contract:

- Names are dotted, `<group>.<verb>`; the command line is the name split on dots.
- **Every mutating command is declared mutating** and refuses to run without `--confirm`. A `write`
  name prefix is optional; if used, only mutating commands may carry it. Ask the user whether they
  want it: it lets prefix-matching permission rules separate reads from writes.
- Reserved group names and flags (contract §17) are unavailable.
- Every list command gets a declared `sort` with a unique tiebreak, a default and maximum `--limit`,
  and a default field set of the few fields an agent needs most.
- Every command with a time dimension accepts `--since` and `--until`, defaulting to the last 24
  hours.

**Default to read-only.** Include a write only if the user asks for it, confirm each one, and set
`destructiveHint` accurately.

Settle the status-code mapping now, from `references/tool-mapping.md`.

---

## Step 4 — Choose identity

- **Language**: Go unless the user asks otherwise. For another language there is no pattern file;
  work from `BASE_TEMPLATE.md` and tell the user the result has had less scrutiny.
- **Binary name**: short, lowercase. **Environment prefix**: the name uppercased.
- **Credentials**: name each one (`API_KEY`, `API_TOKEN`). The names appear in `describe`, `teach
  config`, `doctor`, and every `auth` hint.
- **Settings**: the contract's table plus what this API needs, such as `BASE_URL` or a workspace id.
  Every setting added here appears automatically in `list-config`, the config schema, and `teach
  config`, because all three are generated from one structure.
- **Profiles**: ask which environments or accounts the operator expects. They shape the example
  config file.
- **Build flavours**: with or without server mode (default without), and whether a read-only build is
  needed.

---

## Step 5 — Generate the project

If `references/skeleton/` exists, copy it and substitute `{{MODULE}}`, `{{BINARY}}`, and
`{{ENVPREFIX}}`.

**If it does not, generate from `BASE_TEMPLATE.md` and the language pattern, and tell the user that
the shared machinery is newly written rather than copied from a proven tree.** The response assembly,
shaping, dataset store, redactor, and resolver then deserve closer review than the API-specific parts.

Either way the project gets the full substrate: registry with startup validation; the single response
path; `--fields`, empty-stripping, and size caps; time windows; paging that fills `--limit`; datasets
and the `dataset` commands; write gating; credential resolution, the forbidden-flag scan, permission
checks, and the redactor; the seven-tier settings resolver; `--human`; the discovery, `teach`, and
diagnostic commands; server mode behind a build flag; and the conformance tests.

Build the settings resolver and the redactor first. Most of the rest reads from one or the other.

---

## Step 6 — Wire the upstream adapter

Detect the API's pagination style with `references/tool-mapping.md` and implement the adapter so the
tool can fill `--limit` across pages, and so `--all` can fetch in the largest page size the API
allows.

**Never invent field names.** Use only fields seen in documentation, an OpenAPI schema, or a real
response the user supplied. Where a shape is unknown, leave a stub with a TODO and report it in
Step 11. A plausible wrong field name compiles and then fails against real data.

Define each list command's `fields.available` from the same source, and its `fields.default` as the
handful an agent needs most — usually an id, a name, a status, and a timestamp.

---

## Step 7 — Wire `doctor`

Map the contract's checks onto this API:

- **auth**: the cheapest authenticated read, ideally one that returns the caller's identity. Never a
  write.
- **credentials**: every declared credential, reported by source and file permissions only.
- **network**, **clock**, **datasets**, **writes**: generic; no API-specific work beyond the host.

Ask the user for the identity endpoint if it is not obvious.

---

## Step 8 — Assemble `teach`

Vendor both templates unchanged, with the drift test from the pattern file:

- `orientation.md.tmpl`: bare `teach`. Supply only its domain parts: a two-to-three-sentence summary,
  the handful of commands that answer the most common questions, and the traps a first-time caller
  would hit. Keep the rendered result under 4 KB.
- `shared.md.tmpl`: `teach contract`. Never edit it per tool.

`flags` and `config` are generated from the flag table and settings structure. Make sure
`teach config` renders a complete example config file with at least one profile setting more than
one value, and says plainly that credential entries are paths.

New tools support profile descriptions (contract 1.4.1, §13.4 rule 6): give every profile in the
example config a one-line `description`, and have the `teach config` profiles aspect explain it.

When profiles stand for separate identities (one bot or one account per profile), mark that credential
profile-scoped (contract 1.4.2, §12.1): resolve it from the profile scope only, set
`profile_scoped:true` in `describe`, and say so in the `teach config` credentials aspect. Otherwise
leave it unmarked and keep the shared fallback.

Write domain topics as Markdown files compiled into the binary: query idioms, identifier formats,
common multi-step tasks, known quirks.

---

## Step 9 — Build and verify

```bash
bash scripts/verify_conformance.sh ./bin/<binary> <PREFIX>
```

It runs the automatable items in `references/conformance-checklist.md`. If the tool is built in more
than one flavour (with server mode, read-only), run it against every build: enabling server mode must
not change how the tool behaves on the command line. Then confirm by hand, against
a fake upstream that counts requests:

- `--limit 60` against 20-per-page upstream returns 60 records from 3 requests.
- A byte-capped response resumes at the first dropped record through `next_cursor`.
- `--all` makes fewer upstream requests than paging at the same `--limit`, and reading the dataset
  to the end makes none.
- A write without `--confirm` sends nothing; `--dry-run` sends nothing.
- A credential planted in an upstream response reaches no output and no dataset file.
- A partial profile keeps lower-tier values for the settings it leaves out.
- Dataset expiry, with an injected clock.

Fix every failure before continuing.

---

## Step 10 — Package

A static build; a minimal non-root container image with `<PREFIX>_CONFIG` and `<PREFIX>_DATASET_DIR`
set; a writable dataset volume; cross-compile targets; checksums; and an operator README listing every
setting, every credential, and how to supply each.

For a build with server mode, the README must also cover remote access (contract §16.3): loopback
needs nothing; a remote listener needs `--allow-remote`, a token, and either a certificate and key
(`mkcert` is enough for personal use) or `--tls-terminated-upstream` behind a proxy, mesh, or ingress.

---

## Step 11 — Report

- Files generated, grouped by module.
- The command table from Step 3, as built.
- The credentials and settings the operator must supply.
- Every TODO, especially unknown response fields from Step 6.
- Conformance results: passed, failed, and not checked.

---

## Verification

| Check | Command | Expected |
|---|---|---|
| Builds | `go build ./...` | exit 0 |
| Automated conformance | `bash scripts/verify_conformance.sh <binary> <PREFIX>` | no failures |
| Discovery with nothing configured | `env -i PATH=$PATH <binary> tools` | exit 0, sorted names |
| Orientation size | `<binary> teach \| wc -c` | under 4096 |
| Orientation points onward | `<binary> teach \| grep 'teach contract'` | a match |
| Secret flag refused | `<binary> cards list --api-key x` | exit 8, `secret_on_argv`, `x` not echoed |
| Write gated | `<binary> cards create …` (a mutating command) | exit 8, `preview` present |
| Inline credential rejected | credential value in the config file | exit 3, value not echoed |
| Provenance | `<binary> list-config` | exact variable names or file keys as `source` |
| `--human` keeps the exit code | `<binary> cards get missing --human; echo $?` | 5 |

---

## Notes

- The contract governs. Where this skill and `CONTRACT.md` disagree, fix the skill.
- To add a command later, add a registry entry. Anything outside the registry falls out of
  discovery, `teach`, and server mode.
- An API with no paging is still paged from the agent's side: fetch once, serve `--limit` locally,
  and store it with `--all`.
- Report `total: null` rather than an estimate when upstream gives no count.
- An inline credential in the config file is a startup failure, not a warning. Keep it that way; as
  a warning, the guarantee that the file is safe to read disappears.
- Server mode off by default, but the substrate implements it in full so enabling it later is a
  build flag.

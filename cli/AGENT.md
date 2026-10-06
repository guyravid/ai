# Agent-Facing CLI Guidelines

## Overview

This directory holds the contract for command-line tools that give an automated agent access to a
remote API, the guidance for building one, and a skill that scaffolds one.

The problem is recurring: an agent needs data from a remote API, and handing it the credentials puts
them in its context, transcript, and shell history. A conforming tool keeps the credentials in its
own environment and returns only results. Because every conforming tool behaves the same way, an
agent that has learned one has learned them all.

```
cli/
  AGENT.md                     # this file — advisory
  CLAUDE.md                    # pointer to this file
  contract/
    CONTRACT.md                # normative: what a conforming tool does, and why
    history/                   # append-only record of contract changes
  base/
    BASE_TEMPLATE.md           # how to build one, in any language
    teach/orientation.md.tmpl  # bare `teach`
    teach/shared.md.tmpl       # `teach contract`
    patterns/envelope.md       # schemas, worked examples, cursor format
    patterns/go.md             # the Go realization
  skills/
    scaffold-api-cli/          # generates a new conforming tool
```

**Where this document and `contract/CONTRACT.md` disagree, the contract governs.** Everything here is
a recommendation.

## How the Pieces Relate

| Piece | Role |
|---|---|
| `contract/CONTRACT.md` | Binds observable behaviour only. Names no language. Holds no record of its own past |
| `contract/history/` | That record: one file per contract change, in the same commit, never edited afterwards |
| `base/BASE_TEMPLATE.md` | How to satisfy the contract, including what the contract deliberately leaves out: retry policy, packaging |
| `base/teach/` | The two `teach` templates every tool vendors unchanged |
| `base/patterns/` | The wire format any language must match, and the Go realization |
| `skills/scaffold-api-cli/` | Reads all of the above and produces a new tool, plus the conformance checklist and verifier |

A useful test for where something belongs: if it can be checked by running the binary and reading
its output, it is contract; if it stops being true when the implementation changes, it is base
template; if it stops being true when the language changes, it is a pattern.

## Using a Conforming CLI

Recommendations for an agent calling one of these tools.

- On first contact, run bare `teach`. It is short, names the commands that answer most questions,
  and points to `teach contract` for the rules shared by every tool. Read `teach contract` once per
  family, not once per tool.
- For discovery, go only as deep as needed: `tools` for names, `tools --detail` for one-liners,
  `describe <name>` for one schema. Bare `describe` returns every schema and is rarely worth it.
- Check the exit code before parsing. It distinguishes the failure classes without spending tokens
  on the body.
- On `auth`, `config`, or `network`, run `doctor` before retrying or reporting. It separates "never
  configured" from "rejected" from "unreachable", which the error code alone does not.
- Read `error.retriable` rather than inferring a retry policy, and prefer running `error.hint`
  as-is when it is a command.
- Use `--fields` generously. Asking for three fields instead of the default set is usually the
  single biggest saving available.
- Ask for what you need with `--limit`; the tool pages upstream itself to fill it. To continue, pass
  `meta.page.next_cursor` back alone.
- When you know you need a whole result set, use `--all` and then read the dataset, either with
  `dataset read` or directly from `meta.dataset.path` with line tools such as `jq`. It is cheaper
  than paging through upstream by hand.
- Check `meta.window.source`. When it says `default`, the tool searched only the last 24 hours.
- Read `meta.warnings`, and treat `meta.page.truncated` as "more exists", not as an error.
- Writes need `--confirm`. `tools --detail` marks which commands write (`mutates`). Run once without
  `--confirm` to see the exact request in the refusal's preview, then confirm.
- `--human` is for a person at a terminal. Leave it off when parsing.
- There is no reason to pass a credential as an argument; a conforming tool refuses it and treats the
  credential as leaked.

## Building One

- Read `contract/CONTRACT.md` first, then `base/BASE_TEMPLATE.md`, then the language pattern.
  Reading them in the other order tends to satisfy the guidance and miss a requirement.
- Registry first. A new capability is best added as a registry entry rather than as new argument parsing. The moment a
  command exists outside the registry, discovery, `teach`, and server mode stop agreeing with the
  command line.
- Derive schemas from types rather than writing them. A hand-written schema drifts silently.
- Keep handlers pure: no printing, no envelope building, no environment reads, no dataset access.
- Build the substrate with server mode fully implemented even if the first tools ship without it.
  Turning it on for a tool is then a build flag, not a project.
- Run the conformance checklist in `skills/scaffold-api-cli/references/` before shipping. It is
  derived from the contract and is cheaper than finding the same failures through an agent.
- Update a tool's domain `teach` topics in the same change as the commands they describe.

## Operating One

**Packaging.** The contract says nothing about packaging, but a tool is only useful if it can be
dropped into wherever the agent runs:

- One self-contained binary, with no runtime, interpreter, or package manager on the host.
- A minimal container image holding only the binary, running as a non-root user.
- Checksums published with each release, and the version stamped in at build time.

**Paths in containers.** Minimal images often have no home directory, and a conforming tool refuses
to guess where its files go. Set `<TOOL>_CONFIG` and `<TOOL>_DATASET_DIR` explicitly and mount the
dataset directory writable. A read-only root filesystem plus that one writable mount is a good
default.

**Credentials.**

- Mount credentials as files rather than baking them into an image. Point at them with
  `<TOOL>_<NAME>_FILE`, or with `<name>_file` or `env_file` in the config file.
- Credential files must be owner-only (`chmod 600`); a conforming tool refuses anything looser.
- Environment variables win over files within the same scope, which is useful for a one-off
  override and surprising if a stale variable is left in the environment. `list-config` shows which
  one is in effect.

**Configuration.**

- A config file is a good place for a deployment's shape: profiles, limits, dataset sizing. It is
  safe to commit, because a conforming tool refuses to start if a credential value appears in it.
- It loads automatically from the platform's config directory under `agentcli/<tool>/config.json`.
  `--config` or `<TOOL>_CONFIG` points elsewhere.
- The environment outranks the file, so an orchestrator can override a file it does not own.
- Profiles are partial overrides; a profile only needs what differs. Unmentioned settings keep their
  lower-tier values.
- `list-config` names the exact variable, flag, or file key behind every value. Use it before
  reasoning about the precedence chain by hand.

**Datasets.** Size `DATASET_TOTAL_BYTES` to a few times the largest result set agents are expected
to collect, and `DATASET_TTL` to at least the longest task that reads one. Datasets hold upstream
data at rest; place the directory where that is acceptable.

**Server mode.**

- Stay on loopback when you can. A remote listener requires a token and TLS, and the tool refuses to
  start without both.
- For personal or home-lab use, the simplest TLS is a self-issued certificate: `mkcert -install` once,
  then `mkcert <hostname> <ip>`, and pass the two files with `--tls-cert-file` and `--tls-key-file`.
  Clients must trust the issuing CA; installing it is the fix for a trust error, never disabling
  verification, which hands the token to anyone able to intercept the connection.
- Behind an ingress, service mesh, or reverse proxy that already terminates TLS, use
  `--tls-terminated-upstream` instead. It is a statement about your deployment the tool cannot
  check, so use it only where the hop from the proxy to the tool really is private.
- Serve a **read-only build** whenever the server is reachable by anything other than a trusted
  local agent. Agent permission rules written for the command line never see MCP calls, so a
  read-only build is the only write protection that survives the transport.
- Command names do not have to show whether they write (contract 1.1), so a permission rule that
  matches only the start of a command line cannot tell reads from writes. Where that separation
  matters, deploy the read-only build, or deny each mutating command listed by `tools --detail`.

**Smoke test.** Run `doctor` after every deployment change. It is the cheapest confirmation that
credentials, network, permissions, clock, and the dataset directory are all right.

## Language Stance

The contract names no language. The base template's patterns assume **Go**.

Go is the default because it produces a single static binary with no runtime to install, starts fast
enough that shelling out dozens of times is unremarkable, and cross-compiles to any target without a
toolchain on the build host.

**.NET AOT** and **Rust** are equally acceptable and have the same properties. Any language that
produces a self-contained binary qualifies. Interpreted languages work for a prototype but make the
runtime a dependency of every environment the tool lands in.

## Why the Skill Lives Here

`skills/scaffold-api-cli/` sits in this bundle rather than the repository's top-level `skills/`
because it is a projection of the contract: it turns the contract's rules into generation steps and
tests. Filed elsewhere, it would drift from the specification, and a contract change would leave it
stale with nothing to signal it. It can still be installed from this path.

## Maintenance

- Every contract change gets a file in `contract/history/` in the same commit: frontmatter with date,
  version, change class, and any superseded entry; then Summary, Changes, Rationale, and Migration.
  Corrections go in new files rather than edits to old ones. The `change` class is one of:
  - `initial`: the first version of the contract.
  - `patch`: adds only optional behaviour (MAY), as contract §18 defines, so every tool conforming
    to the previous version still conforms unchanged; bumps the patch version.
  - `minor`: adds something, as contract §18 defines; bumps the minor version.
  - `major`: changes the meaning of an exit code or flag, or the envelope's shape (§18); bumps the
    major version.
  - `editorial`: changes no rule. Fixes an example, a cross-reference, or wording, so that the text
    agrees with the rules it already states. The version stays the same, and Migration says
    whether anything copied from the old text needs fixing.

  Choose by effect, not by size. A one-word change that alters what a tool MUST do is `minor` or
  `major`. A change is `patch` only if no existing tool needs to change.
- A change to the envelope, error codes, flags, or settings touches: the contract,
  `patterns/envelope.md`, `BASE_TEMPLATE.md`, both `teach` templates, the conformance checklist, and
  the verifier. Change them together; drift between them is the most likely defect in this bundle.
- Update the conformance checklist in the same change, then run
  `skills/scaffold-api-cli/scripts/check_checklist.sh`.
- When the skill triggers on the wrong requests, revise its `description` before its body.
- Command names are dotted (`cards.list`), which differs from the verb-first underscore convention in
  `../mcps/CLAUDE.md`. Dotted names map to the command line without a lookup table and are valid MCP
  tool names. The divergence is deliberate.

### Release tags

Every change to the contract or to a tool under `tools/` ends with a tag. Tags are
`<scope>/v<MAJOR>.<MINOR>.<PATCH>`, where scope is `contract` or the tool's directory name:
`contract/v1.4.2`, `trello/v1.1.1`, `telegram/v1.1.0`. A tool's build reads its version from its
own tag (prefix stripped), so a missing tag means the binary reports a commit hash instead of a
version.

When a change touches either, the agent:

1. Finds the current version: `git tag --list '<scope>/v*' --sort=-v:refname | head -1`.
2. Proposes the next one and says why:
   - Contract: the version and `change` class from the new `contract/history/` entry. The tag must
     match the version in `CONTRACT.md`. `editorial` changes get no tag.
   - Tool: major if a command, flag, field default, or exit code changes in a way that breaks
     existing callers; minor if it adds commands, flags, or fields; patch for fixes and docs.
     If the tool now conforms to a new contract version, bump at least minor.
3. Once the change is merged to `main`, reminds the user to tag the merged result and push, with
   the exact commands:

   ```sh
   git checkout main && git pull
   git tag -a <scope>/vX.Y.Z -m "<scope> vX.Y.Z: <one-line summary>"
   git push origin <scope>/vX.Y.Z
   ```

   One tag per changed scope. A change to the contract and to two tools gets three tags.

   Tag on `main`, not on the branch. A squash or rebase merge leaves a branch commit off `main`,
   and builds from `main` then report a commit hash instead of the version. Merge PRs that carry a
   tagged commit with a merge commit, not a squash.

The agent suggests these commands and never runs them itself. Tagging and pushing stay with the
user.

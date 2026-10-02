# Planning Prompt: Agent-Facing CLI Contract System

> This document is a consolidated task brief, not an implementation plan. Hand it to a
> planning-capable model to produce the actual step-by-step implementation plan. Do not
> skip straight to code from this document alone.

## Objective

Design and build a standard **contract** for how AI-agent-facing, API-querying CLIs get
built — plus the scaffolding to produce new CLIs that follow it. The whole system (contract,
methodology, and the skill that scaffolds new CLIs from it) lives together as a single bundle
in `cli/` at the repo root (`/Users/guy-ravid/Projects/ai/cli`), alongside this repo's other
top-level bundles (`agents/`, `mcps/`, `skills/`).

The recurring problem being solved: an AI agent needs to query some external API (support
ticketing, monitoring, incident management, etc.) as part of its work. Today that either means
loading API keys into the agent's context (a secrets-exposure risk) or hand-rolling a bespoke
integration each time. The fix is a CLI the agent shells out to — credentials live in the CLI's
own execution environment, never in agent context — built to a **consistent contract** so every
future CLI (regardless of which API it wraps) behaves the same way an agent already knows how to
use.

The first concrete CLI to build against this contract is **not yet decided** — an earlier OpsGenie
mention was just a placeholder example and should be ignored. This planning pass is scoped to the
contract system itself, not any specific API integration.

## Deliverables to plan

1. `cli/contract/CONTRACT.md` — the **what and why**. Dry, unopinionated spec. No methodology,
   no prose about how to implement anything, no embedded version history.
2. `cli/contract/BASE_TEMPLATE.md` — the **how**. Baseline methodology and shared implementation
   patterns for a CLI built under the contract — the substrate a new CLI's implementation starts
   from (e.g. how the `teach` command assembles its output, how paging/disk-spill is structured,
   how secrets are loaded, output envelope conventions).
3. `cli/contract/history/` — an append-only trailing log of edits made to the contract over time.
   Chronological, never rewritten in place. This is where "previous versions" info lives instead
   of inside `CONTRACT.md`.
4. A meta doc at the `cli/` level — `README.md` or `AGENTS.md`. Repo precedent leans
   `AGENTS.md`/`CLAUDE.md` for agent-facing meta docs (see `agents/Claude.md`, `mcps/CLAUDE.md`,
   `mcps/agent.md`) — the planner should pick a name consistent with that convention unless
   there's a reason not to.

   Role: where `CONTRACT.md` specifies *what* a conforming tool does and *why* each rule exists
   — taking no position on practice — the meta doc is the *how*: the methodology for **using** a
   tool in this family, for **building** one, and for **operating** one. It also documents how
   the four pieces (contract, base template, history, skill) relate to each other.

   It is strictly advisory. Nothing in it overrides the contract. Write it in recommendation
   language — "prefer", "typically", "recommended" — never in the contract's register, which is
   implicitly imperative throughout. If `CONTRACT.md` says *must*, this document does not get a
   vote.

   This is also where the **language stance** lives: the contract stays language-agnostic, but
   the reference implementation and `BASE_TEMPLATE.md` assume **Go** as the default (single
   static binary, fast, trivially cross-compiled, no runtime to install), with .NET AOT and Rust
   noted as acceptable alternatives — that rationale and default-language call belongs in this
   meta doc, not in `CONTRACT.md`.
5. A **skill** that scaffolds a new CLI according to the contract, living inside `cli/` itself
   (e.g. `cli/skills/<name>/SKILL.md` + supporting scripts/references) — bundled with the contract,
   not filed under the repo's top-level `skills/`. This is the thing an agent invokes when asked
   to "build a CLI for X API": it reads CONTRACT.md + BASE_TEMPLATE.md and produces a new,
   contract-compliant CLI project.

`cli/contract/` already exists as an empty directory — nothing is tracked in git yet.

## Contract requirements (what / why)

Fold these into `CONTRACT.md` as dry, declarative requirements:

- **Purpose**: give an AI agent a consistent, reusable way to call an external API through a
  local CLI, without ever loading API keys/secrets into the agent's context window.
- **Secrets handling**: the CLI reads credentials from its own execution environment (env vars,
  mounted secret files — typical in a container). Credentials are never accepted as CLI
  arguments (shell-history / transcript leak risk) and never echoed in output.
- **Packaging**: must be trivial to attach to a machine or agent runtime, including inside a
  container — drag-and-drop, minimal/no runtime dependencies.
- **Language stance**: `CONTRACT.md` itself must stay language-agnostic — it must not name or
  require a specific language. The default/reference language choice and its rationale are
  documented in the meta doc (see Deliverable 4), not here.
- **Baseline commands** every contract-compliant CLI must implement:
  - `teach` — emits a **human-readable Markdown** document that teaches an agent how to use
    *this specific* CLI. Its shared/common content is drawn from `BASE_TEMPLATE.md`'s
    methodology; the CLI-specific portion covers that CLI's own domain and commands.
  - `tools` — capability index: terse list of available commands/tools, names only (or name +
    one-liner) — analogous to an MCP `list_tools` call.
  - `tools --detail` — same list, with a short description per tool.
  - `describe tools` — full MCP-style detailed output per tool: complete schema (params, types,
    required/optional, output envelope shape). Exact subcommand/flag spelling is an
    implementation detail the planner can finalize, but the three-tier
    index → short-detail → full-schema semantics must be preserved.
- **Run modes**: beyond direct one-shot CLI invocation (an agent shelling out per command), the
  CLI must also be runnable as an **MCP server**, over both the **stdio** transport and **HTTP**
  transport (transport selection via flag/subcommand — exact spelling is the planner's call).
  MCP mode exposes the same commands as MCP tools, reusing the tool/schema definitions that back
  `describe tools` — no separate, divergent tool-definition source of truth.
- **Paging**: any command returning a list/dataset accepts paging parameters (page/cursor +
  size). The CLI's job is to project the requested page — it does not manage paging state for
  the agent beyond that.
- **Full-dataset / large-result handling**: when an agent wants an entire dataset (to cut down
  round-trips), the CLI may write the full result set to a local disk cache file keyed by a
  GUID, and return only a page of that data in its response envelope alongside the GUID and
  position/cursor — so the agent can page through the cached file on subsequent calls instead of
  re-querying the upstream API or receiving the whole payload over stdout in one shot.
  - This validates a concern raised during scoping: dumping large JSON payloads straight to
    stdout is a real problem — it inflates agent context/token usage and is wasteful when the
    agent only needs to consume data incrementally. The disk-spill + GUID + envelope pattern
    keeps the CLI stateless across invocations while still giving the agent a cheap handle to a
    large result set.
  - **Cache retention policy**: TTL since last access, **and** a configurable max total cache
    size on disk, with oldest-entries-evicted-first once the cap is exceeded. Both the TTL
    duration and the max size must be configurable (mechanism — env var, flag, config file — is
    the planner's call).
- **Versioning discipline**: `CONTRACT.md` never references its own prior versions or contains
  changelog prose. All edit history is appended to `cli/contract/history/`.

## Base template requirements (how)

`BASE_TEMPLATE.md` is the shared implementation substrate every new CLI starts from — where the
meta doc's "building one" methodology (recommendation-level, prose) gets operationalized into
concrete, reusable patterns a new CLI's code is actually built from. It should cover, at minimum:

- How the `teach` command is assembled: shared/common methodology content (sourced from this
  template) plus CLI-specific content.
- How the `tools` / `tools --detail` / `describe tools` tiers are implemented consistently.
- How MCP server mode is implemented for both stdio and HTTP transports, reusing the same
  command/tool definitions that back `describe tools` rather than maintaining a second copy.
- How paging parameters are accepted and projected.
- How the disk-spill/GUID/envelope mechanism for large datasets is structured, including the
  TTL + size-cap cache eviction logic.
- How secrets are loaded from the environment and kept out of logs/output.
- Response envelope conventions shared across all commands (success/error shape, pagination
  metadata shape, etc.).

`CONTRACT.md` is the what/why; `BASE_TEMPLATE.md` is the how. Keep the split strict — don't let
methodology leak back into `CONTRACT.md`.

## Skill requirements

- Lives inside `cli/` (single bundle with the contract) — not under the repo's top-level
  `skills/`.
- Purpose: given a request to build a CLI for some API, scaffold a new contract-compliant CLI
  project by reading `CONTRACT.md` and `BASE_TEMPLATE.md`.
- Should produce a project that already has the baseline commands (`teach`, `tools`,
  `tools --detail`, `describe tools`), working MCP server mode (stdio + HTTP transports), the
  envelope/paging/disk-spill machinery from the base template, and slots for the CLI-specific
  commands/domain logic.

## Explicitly out of scope for this planning pass

- Choosing or building the first real target CLI (e.g. an OpsGenie integration). That comes
  later, after this contract/template/skill bundle exists.
- Producing the actual implementation plan — that's the job of whatever planning-capable model
  receives this document next.

## Repo context

- Working repo root: `/Users/guy-ravid/Projects/ai` (remote: `github.com/guyravid/ai`).
  `cli/` is a top-level directory in that repo, alongside `agents/`, `mcps/`, `skills/`.
- `cli/contract/` already exists, empty, untracked.
- Existing agent-facing meta-doc precedent in this repo: `agents/Claude.md`, `mcps/CLAUDE.md`,
  `mcps/agent.md`.

## Source material

Raw, chronological notes this brief was consolidated from (OpsGenie mention should be ignored —
first target CLI is still undecided):

> [2026-08-07, 15:23:03] Work on a skill for @Skills/ that will create a cli based API utility
> that will be used by an ai agent to query data. The goal of the skill is to setup a standard
> way of doing it. The point of the CLI is to be able to call the API consistently without
> loading keys into the AI context space, by giving access to claude cli easily, and to be
> packaged to easily be attached to an machine or agent cli (in a container for example). The
> skill should work for any programming language the user desires, but I am leaning to go for
> it's performance and for the fact that once compiled it can be dragged and dropped in. This is
> an open question and we should explore this together. The first cli I want to build is one for
> OpsGenie.
>
> [2026-08-07, 15:23:34] I want to make a few changes to the structure of this project before we
> continue. Move the CONTRACT into it's own directory, and then remove from the contract all
> references to previous versions. Add a directry next to Contract.md that creates a trailing
> list of edits made for a full history, rather than have it live in the CONTRACT.md.
> Additionally, there are a lot of assumptions made in the methodology of using the CLI both in
> the CONTRACT (which should be emotionless), and this session. Create a BASE_TEMPLATE.MD, that
> will serve as a solid baseline for an CLI being implemented using this contract, and will
> contain the methodology of using the CLI. The contract is the what and why, the base template
> is the how.
>
> [2026-08-07, 15:24:36] To be added: Make sure there is a teach command. Include a skill in the
> infrastructure not as separate.
>
> [2026-08-10, 10:28:48] Another element to be made to the contract is the ability to support
> large data subsets. The cli should support paging as a parameter (the agent may want to manage
> paging itself, so the cli just projects the information). But there can be a scenario where the
> agent knows it wants a whole dataset. In order to lower the communications back and forth, so
> the CLI can handle communication however makes sense for it, the CLI can write to disk all the
> data in a file with guid for a name, and instead of returning everything out once which may
> overload stdout (let me know if this is a valid concern), it can return a subset of the data
> with the guid and position in the envelope. The file is retained for however long after there
> are calls made to the CLI with requests for that data. This allows the cli to run stateless,
> while still retaining a sort of state. The maximum file size of the state should be
> configurable.

## Resolved during clarification (do not re-ask)

- **Language**: `CONTRACT.md` stays language-agnostic. The default/reference implementation
  (Go, with .NET AOT and Rust as acceptable alternatives) and its rationale live in the meta doc
  (`README.md`/`AGENTS.md`), not in `CONTRACT.md`.
- **`teach` output**: human-readable Markdown, built from `BASE_TEMPLATE.md`'s shared content.
- **Discovery tiers**: `tools` (index) → `tools --detail` (short description) → `describe tools`
  (full MCP-style schema).
- **Cache retention**: TTL since last access + total cache size cap, oldest evicted first.
- **Bundle location**: everything (contract, base template, history, skill) lives under `cli/`,
  not split across the repo's top-level `skills/` directory.

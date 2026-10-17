# deals-orchestrator

Interactive front door for the deals tracker. Everything it writes goes through the `deals` CLI in `runner/`.

## Run

```
cd agents/deals-orchestrator
claude
```

`.claude/settings.json` sets `"agent": "deals-orchestrator"`, so a plain `claude` here starts as the orchestrator, the same as `claude --agent deals-orchestrator`. It must be the main session; subagents cannot spawn subagents.

## Roster

| Agent | Role | Tools |
|---|---|---|
| deals-orchestrator | Talks to the user, owns writes via `deals` | Agent, Bash (deals only), Read |
| spec-refiner | Builds specs, compiles briefs, compacts notes | Read |
| product-resolver | Confirms exact products, proposes candidate models | WebSearch, WebFetch |
| deal-searcher | Judges listings, checks one URL, sweeps | WebSearch, WebFetch |

The scheduled runner (`runner/`) calls deal-searcher with `claude -p`; see the plan in `plans/deals-agent-plan.md`.

## Running

The agents live in this bundle's `.claude/agents/`, so Claude Code picks them up for any session started here. Nothing is installed globally.

The `deals` CLI must be on PATH (for example `ln -s "$PWD/runner/src/cli.ts" ~/.local/bin/deals`), and the config goes at `~/.config/deals/config.json` (see `runner/config.example.json`). The scheduled runner reads `.claude/agents/deal-searcher.md` directly, so it doesn't depend on the session setup.

## Working on the agent

Because of the `agent` setting, a session started here is the orchestrator, not a general Claude Code session. How each start behaves:

| How it's started | Result |
|---|---|
| `claude` in this directory | Runs as deals-orchestrator |
| `claude --setting-sources user` | Normal Claude Code session, project settings skipped |
| `claude --settings '{"agent":"general-purpose"}'` | Normal session, project settings kept |

Three ways to work on the agent itself:

- **Open from `agents/`** (recommended). Project settings come from the directory the session starts in, so the `agent` setting doesn't apply, and `agents/AGENTS.md` (the authoring guidelines) loads.
- **Stay here and skip project settings** with `claude --setting-sources user`. This also skips the bundle's permissions, which is fine for editing.
- **Use an alias** if you do this often, e.g. `alias claude-dev='claude --setting-sources user'`.

## Models per mode

deal-searcher runs on Sonnet for all three modes (`evaluate-listings`, `check-url`, `sweep`), with default thinking. This is configurable in the runner. The runner enforces the output contract with `--json-schema` (schemas in `runner/src/llm/schemas.ts`).

## Model eval

`runner/eval/` scores deal-searcher `evaluate-listings` against labelled synthetic listings on any model (`--model`). The thinking setting comes from `MAX_THINKING_TOKENS` in the environment and is printed in the report. It calls `claude -p` and costs money; run it deliberately:

```
cd runner
node eval/run-eval.ts --model sonnet --max-budget-usd 1.00
```

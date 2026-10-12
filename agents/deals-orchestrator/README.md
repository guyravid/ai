# deals-orchestrator

Interactive front door for the deals tracker. Everything it writes goes through the `deals` CLI in `runner/`.

## Run

```
claude --agent deals-orchestrator
```

It must be the main session; subagents cannot spawn subagents.

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

```
cd agents/deals-orchestrator
claude --agent deals-orchestrator
```

The `deals` CLI must be on PATH (for example `ln -s "$PWD/runner/src/cli.ts" ~/.local/bin/deals`), and the config goes at `~/.config/deals/config.json` (see `runner/config.example.json`). The scheduled runner reads `.claude/agents/deal-searcher.md` directly, so it doesn't depend on the session setup.

## Models per mode

deal-searcher runs on Sonnet for all three modes (`evaluate-listings`, `check-url`, `sweep`), with default thinking. This is configurable in the runner. The runner enforces the output contract with `--json-schema` (schemas in `runner/src/llm/schemas.ts`).

## Model eval

`runner/eval/` scores deal-searcher `evaluate-listings` against labelled synthetic listings on any model (`--model`). The thinking setting comes from `MAX_THINKING_TOKENS` in the environment and is printed in the report. It calls `claude -p` and costs money; run it deliberately:

```
cd runner
node eval/run-eval.ts --model sonnet --max-budget-usd 1.00
```

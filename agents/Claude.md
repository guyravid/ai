# Agent Development Guidelines

## Overview
This directory contains Claude Code subagent definitions — custom agents spawned via the `Agent` tool using `subagent_type`. Each subdirectory is a self-contained agent with its own definition file and supporting references.

## What is a Subagent
A subagent is a Claude instance spawned with a specialized system prompt, a curated tool set, and a focused scope. The parent model delegates a task to it via the `Agent` tool. The subagent runs independently, returns a single result, and has no persistent state beyond what's in its prompt.

## Project Conventions

### Structure
Each agent lives in its own subdirectory (`agents/<agent-name>/`) and should follow this layout:
```
<agent-name>/
  AGENT.md          # The agent definition — loaded as the system prompt
  README.md         # Optional: usage notes, examples, known limitations
  references/       # Optional: reference docs, examples, or lookup tables the agent reads
```

### Agent Definition File (`AGENT.md`)
The definition file is the system prompt. It must include:

**Frontmatter** (required):
```yaml
---
name: <agent-name>
description: <one-line description used for routing and tool selection>
tools: [Bash, Read, WebFetch, ...]   # explicit allowlist
---
```

**Body sections** (adapt as needed):
- **Purpose** — one paragraph on what this agent does and why it exists as a separate agent.
- **Trigger conditions** — when should the parent model spawn this agent vs. handle inline.
- **Instructions** — step-by-step behavior. Numbered for determinism; use headers for phases.
- **Output format** — exactly what the agent must return. Be explicit: length, structure, what to omit.
- **Constraints** — what the agent must never do (e.g., write files, make commits, call external APIs).

### Naming
- Directory and `name` field: `kebab-case`, descriptive, noun or noun-phrase (e.g., `dependency-auditor`, `log-analyzer`).
- Avoid generic names (`helper`, `assistant`, `agent`) — name for the specific capability.

### Tool Allowlist
- List only tools the agent genuinely needs. Fewer tools = less surface area = more predictable behavior.
- Never give write tools (`Edit`, `Write`, `Bash` with mutations) to read-only agents.
- If the agent only reads, say so in both the frontmatter and the `Constraints` section.

### Scope
- One agent, one responsibility. An agent that does two things should be two agents.
- If an agent needs context from another, pass it in the prompt — don't chain agents implicitly.
- Keep agents stateless: they receive full context in the prompt and return a result. No side channels.

### Instructions Quality
- Write instructions imperatively: "Read X, then summarize Y" not "The agent should read X".
- Anticipate failure modes: what should the agent do if the file doesn't exist, the API is down, the query returns nothing?
- End with an explicit output contract — what a well-formed response looks like.

### Output Format
Define the expected output precisely:
- Max length (e.g., "under 300 words", "a markdown table with N columns")
- What to include vs. omit
- How to signal confidence or uncertainty
- How to signal blockers (e.g., "If X is missing, return: `BLOCKED: <reason>`")

## When to Create a New Agent
Create a new agent when:
- The task requires a different tool set than the parent or existing agents
- The task has a well-defined, reusable scope that recurs across conversations
- Isolating the subtask protects the parent's context window from noisy or large results
- The subtask benefits from a different persona or instruction set (e.g., a strict read-only auditor)

Don't create an agent for:
- One-off tasks that won't recur
- Tasks that are a single tool call
- Tasks where inline handling is simpler and equally clear

## Testing an Agent
- Manually invoke the agent via the `Agent` tool with a representative prompt before committing.
- Test the happy path and at least one failure path (missing input, ambiguous query, no results).
- Verify the output format matches the contract defined in `AGENT.md`.
- If the agent reads files or calls external tools, test against realistic fixture data.

## Maintenance
- When the parent model's behavior changes in a way that affects routing, update the `description` field.
- When the tool allowlist changes (tools added/removed from Claude Code), audit each agent's frontmatter.
- Review agent output contracts periodically — drift between the defined format and actual output causes silent failures.
- If an agent is no longer used, remove it rather than leaving it stale.

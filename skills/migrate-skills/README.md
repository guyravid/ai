# migrate-skills

A Claude Code skill for migrating or linking Claude resources — skills, agents, and MCP servers — from a shared source into your Claude configuration.

## What it does

Point it at a root directory (e.g., a team repo) and it finds, confirms, and migrates all three resource types in one pass:

- **Skills** — detected by `SKILL.md`; installed as a directory (symlink or copy) under `~/.claude/skills/`
- **Agents** — detected by `AGENT.md`; installed as a single `.md` file under `~/.claude/agents/`
- **MCPs** — detected by directory name (contains "mcp"); registered via `claude mcp add` with user confirmation
- **Conflict handling** — detects existing resources (linked or copied), diffs them, and lets you decide how to proceed
- **Three install scopes** — global, user, or project for skills and agents; user or project scope for MCP registration
- **Cloud account sync** (opt-in, skills only) — compares local skills with your Anthropic cloud account and surfaces discrepancies

## How to use it

```
/migrate-skills
```

Or describe what you want:

> "Migrate everything from this repo into my Claude setup"
> "Install the team skills and agents from ~/eng/shared-claude"
> "Check if my local skills are up to date with the source repo"

The skill discovers all candidates, asks you to confirm (especially for MCPs), then guides you through the rest.

## Installation

To install this skill itself, run `/migrate-skills` from this repo — it will find and walk you through the install.

Or manually:

```bash
# Symlink (recommended)
ln -s $(pwd)/skills/migrate-skills ~/.claude/skills/migrate-skills

# Or copy
cp -r skills/migrate-skills ~/.claude/skills/migrate-skills
```

## Skill file

The skill logic lives in [SKILL.md](./SKILL.md).

# migrate-skills

A Claude Code skill for migrating or symlinking skills from a shared source into a Claude skills directory.

## What it does

This skill walks you through installing skills from a shared repo (or any directory) into your Claude setup. It supports:

- **Symlinking** — recommended for shared repos; source updates apply automatically
- **Copying** — standalone copies that you own and control
- **Selective migration** — pick individual skills or migrate all at once
- **Three install targets** — global (`/.claude/skills/`), user (`~/.claude/skills/`), or project (`.claude/skills/` at git root)
- **Conflict handling** — detects existing skills (linked or copied), diffs them, and lets you decide how to proceed
- **Cloud account sync** (opt-in) — compares local skills with your Anthropic cloud account and surfaces discrepancies

## How to use it

Invoke it in Claude Code:

```
/migrate-skills
```

Or just describe what you want:

> "Migrate the skills from this repo into my Claude setup"
> "Install the team skills from ~/eng/shared-skills"
> "Check if my local skills are up to date with the source repo"

The skill will guide you through the rest interactively.

## Installation

To install this skill into your own Claude setup, run `/migrate-skills` from this repo — it will find itself and walk you through the install.

Alternatively, copy or symlink the `migrate-skills/` directory into your Claude skills directory:

```bash
# Symlink (recommended)
ln -s $(pwd)/skills/migrate-skills ~/.claude/skills/migrate-skills

# Or copy
cp -r skills/migrate-skills ~/.claude/skills/migrate-skills
```

## Skill file

The skill logic lives in [SKILL.md](./SKILL.md).

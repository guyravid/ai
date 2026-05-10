---
name: migrate-skills
description: Migrate or link skills from a shared or team repository into Claude's skills directory. Use this skill when the user wants to install, migrate, sync, copy, or link skills from a shared repo, team directory, or any source path into their Claude setup. Trigger when the user mentions migrating skills, installing skills from a repo, syncing skills, pulling in team skills, or setting up skill links. Also trigger when the user wants to check whether their local skills are up to date with a source.
---

# Migrate Skills

Walk the user through migrating or symlinking skills from a shared source into their Claude skills directory.

## Step 1: Determine the source directory

If the user hasn't provided a source path, ask:
> "Where should I look for skills? I can use the **current directory**, or you can give me a path to the shared repo."

Verify the directory exists. If it doesn't, say so and ask again.

## Step 2: Discover available skills

Run the discovery script and present the results:

```bash
bash scripts/discover_skills.sh <source-dir>
```

Each line is a skill directory. Present the list to the user. If empty, stop.

## Step 3: Scope

Ask: "Do you want to migrate **all** skills, or pick specific ones?"

- **All** → proceed with the full list
- **Specific** → show a numbered list; let the user select by number, name, or mix

## Step 4: Choose the installation target

Ask where to install:

1. **Global** — `/.claude/skills/`
2. **User** — `~/.claude/skills/`
3. **Project** — `<git-repo-root>/.claude/skills/` (detect with `git rev-parse --show-toplevel`)

Create the target `skills/` directory if it doesn't exist.

## Step 5: Choose the default migration method

Ask upfront:
> "How do you want to migrate the skills?
> 1. **Link all** (symlinks — recommended; source updates apply automatically)
> 2. **Copy all** (standalone copies — you own them)
> 3. **Decide per skill**"

This default applies to new skills. Existing skills are always handled individually.

## Step 6: Check and migrate each skill

For each selected skill:

**If it already exists in the target — check whether it's a symlink or directory:**
```bash
test -L <target>/<name> && echo "linked" || echo "copied"
```

- **Symlink:** compare link target to source path. If it matches and `test -e` passes → already correct, skip. If broken or pointing elsewhere → tell user and offer: relink / copy / skip.
- **Directory:** run the comparison script:
  ```bash
  bash scripts/compare_skill.sh <source-skill-dir> <target>/<name>
  ```
  - Exit 0 (identical) → skip.
  - Differences → show `diff -r` output and offer: update / link instead / skip.

Always confirm before overwriting or relinking.

**If it doesn't exist yet:** apply the default method from Step 5 (symlink or copy).

## Step 7: Summary

```
| Skill         | Action                  |
|---------------|-------------------------|
| my-skill      | Newly linked            |
| other-skill   | Already linked (skipped)|
| third-skill   | Updated (copied)        |
| fourth-skill  | Skipped by user         |
```

---

## Phase 2: Cloud Account Sync (opt-in)

After the summary, ask: "Do you want to also check your Anthropic cloud account for skill discrepancies?"

If yes, read `references/cloud-sync.md` and follow the workflow there.

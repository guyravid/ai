---
name: migrate-skills
description: Migrate or link skills, agents, and MCP servers from a shared or team repository into Claude's configuration. Use this skill when the user wants to install, migrate, sync, copy, or link skills, agents, or MCPs from a shared repo, team directory, or any source path into their Claude setup. Trigger when the user mentions migrating skills, agents, or MCPs, installing from a repo, syncing, pulling in team resources, or checking whether local resources are up to date with a source.
---

# Migrate Skills, Agents, and MCPs

Walk the user through migrating or linking Claude resources — skills, agents, and MCP servers — from a shared source into their Claude configuration. Offer to migrate everything from a root directory in one pass.

## Step 1: Determine the source directory

If the user hasn't provided a source path, ask:
> "Where should I look? I can use the **current directory**, or give me a path to a shared repo or root."

Verify the directory exists. If it doesn't, say so and ask again.

## Step 2: Discover candidates

Run all three discovery scripts against the source directory:

```bash
bash scripts/discover_skills.sh <source-dir>
bash scripts/discover_agents.sh <source-dir>
bash scripts/discover_mcps.sh <source-dir>
```

- `discover_skills.sh` — finds directories containing a `SKILL.md` file.
- `discover_agents.sh` — finds directories containing an `AGENT.md` file.
- `discover_mcps.sh` — finds directories whose name contains "mcp" (case-insensitive). These are **candidates** — always confirm with the user.

## Step 3: Confirm candidates

Show the user what was found, grouped by type:

```
Skills found:
  - /path/to/my-skill
  - /path/to/code-reviewer

Agents found:
  - /path/to/my-agent

MCP candidates (confirm these are MCP servers):
  - /path/to/mcps           ← is this a container or an MCP server itself?
  - /path/to/gemini-mcp
```

For **MCP candidates**, explicitly ask the user to confirm which are actual MCP servers and whether any are containers (parent directories holding multiple MCPs). If a directory is a container, list its immediate subdirectories as the individual MCP servers to migrate.

For **skills and agents**, the `SKILL.md`/`AGENT.md` markers are authoritative — no extra confirmation needed unless the list looks unexpected.

Ask: "Does this list look right? Remove anything that doesn't belong, and tell me if I missed anything."

Incorporate any corrections before proceeding.

## Step 4: Choose resource types to migrate

Ask which types to proceed with:
> "Which do you want to migrate?
> 1. Skills (N found)
> 2. Agents (N found)
> 3. MCPs (N confirmed)
> 4. All of the above"

Proceed only with what the user selects and what has confirmed items.

---

## Part A: Skills

*(Skip if not selected or no skills confirmed.)*

### A1: Scope

Ask: "Do you want to migrate **all** skills, or pick specific ones?"
- **All** → proceed with the full list.
- **Specific** → show a numbered list; let the user select by number, name, or mix.

### A2: Choose the installation target

Ask where to install:
1. **Global** — `/.claude/skills/`
2. **User** — `~/.claude/skills/`
3. **Project** — `<git-repo-root>/.claude/skills/` (detect with `git rev-parse --show-toplevel`)

Create the target `skills/` directory if it doesn't exist.

### A3: Choose the default migration method

Ask:
> "How do you want to migrate the skills?
> 1. **Link all** (symlinks — recommended; source updates apply automatically)
> 2. **Copy all** (standalone copies)
> 3. **Decide per skill**"

### A4: Check and migrate each skill

For each selected skill:

**If it already exists in the target:**
```bash
test -L <target>/<name> && echo "linked" || echo "copied"
```
- **Symlink:** compare link target to source path. If it matches and `test -e` passes → already correct, skip. If broken or pointing elsewhere → tell user and offer: relink / copy / skip.
- **Directory:** run:
  ```bash
  bash scripts/compare_skill.sh <source-skill-dir> <target>/<name>
  ```
  - Exit 0 → skip.
  - Differences → show `diff -r` output and offer: update / link instead / skip.

Always confirm before overwriting or relinking.

**If it doesn't exist yet:** apply the default from A3.

### A5: Skills summary

Present a results table before moving to the next part.

---

## Part B: Agents

*(Skip if not selected or no agents confirmed.)*

Agents install as **single `.md` files** — the `AGENT.md` content — not as directories.

### B1: Scope

Ask: "Do you want to migrate **all** agents, or pick specific ones?"

### B2: Choose the installation target

Ask where to install:
1. **Global** — `/.claude/agents/`
2. **User** — `~/.claude/agents/`
3. **Project** — `<git-repo-root>/.claude/agents/`

Create the target `agents/` directory if it doesn't exist.

### B3: Choose the default migration method

Ask:
> "How do you want to migrate the agents?
> 1. **Link all** (symlinks to the AGENT.md file)
> 2. **Copy all** (standalone copies)
> 3. **Decide per agent**"

### B4: Check and migrate each agent

Derive the agent name from the `name:` field in AGENT.md frontmatter:
```bash
grep '^name:' <source-agent-dir>/AGENT.md | head -1 | sed 's/^name:[[:space:]]*//'
```
Fall back to `basename <source-agent-dir>` if missing.

The target file is `<target-agents-dir>/<name>.md`.

**If the target file already exists:**
```bash
test -L <target>/<name>.md && echo "linked" || echo "file"
```
- **Symlink:** compare link target to source AGENT.md path. If correct and not broken → skip. Otherwise offer: relink / copy / skip.
- **File:** run:
  ```bash
  bash scripts/compare_agent.sh <source-agent-dir> <target>/<name>.md
  ```
  - Exit 0 → skip.
  - Differences → show diff and offer: update / link instead / skip.

Always confirm before overwriting or relinking.

**If it doesn't exist yet:** apply the default from B3.
- **Symlink:** `ln -s <abs-path-to-source-agent-dir>/AGENT.md <target>/<name>.md`
- **Copy:** `cp <source-agent-dir>/AGENT.md <target>/<name>.md`

### B5: Agents summary

Present a results table before moving to the next part.

---

## Part C: MCPs

*(Skip if not selected or no MCPs confirmed.)*

MCPs are **registered** in Claude's configuration rather than copied or symlinked. The source directory stays in place; migration means adding the MCP to Claude's active MCP list.

### C1: Scope

Ask: "Do you want to register **all** confirmed MCPs, or pick specific ones?"

### C2: Choose the registration scope

Ask:
> "Where do you want to register the MCPs?
> 1. **User** — available in all projects (`~/.claude/settings.json`)
> 2. **Project** — current project only (`.mcp.json`)"

### C3: Check existing registrations

Run:
```bash
claude mcp list 2>/dev/null
```

Note which MCPs are already registered. Derive each MCP's name:
- From `manifest.json` if present: `grep '"name"' <mcp-dir>/manifest.json | head -1 | sed 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/'`
- Otherwise from `CLAUDE.md` if it contains a `claude mcp add` snippet (extract the name argument)
- Fall back to `basename <mcp-dir>`

### C4: Register each MCP

**If already registered:** tell the user and offer: re-register (update) / skip.

**If not registered:**

First, check for a `CLAUDE.md` in the MCP directory — it is the authoritative source for how to run the server:
```bash
test -f <mcp-dir>/CLAUDE.md && cat <mcp-dir>/CLAUDE.md
```
If it contains a `claude mcp add` command, show it to the user and use it as-is (with scope flag adjusted).

If no `CLAUDE.md` or no `claude mcp add` snippet:

1. Check `manifest.json` for `mcp_config.command` and `mcp_config.args`.
2. If neither is available, ask the user for the run command.

**For local (stdio) MCPs:**

Check whether a build is needed (e.g., TypeScript source with no `dist/`):
```bash
test -d <mcp-dir>/dist && echo "built" || echo "needs-build"
```
If not built, warn the user and offer to build:
```bash
cd <mcp-dir> && npm run build
```
Only run this after confirmation. Never build without asking.

Then register:
```bash
claude mcp add --scope <scope> <name> -- <command> <abs-path-args...>
```

**For remote (HTTP) MCPs:**

Check `CLAUDE.md` or `README.md` for the deployed URL:
```bash
grep -Eo "https://[^[:space:]]+" <mcp-dir>/README.md | head -1
```
If unresolved, ask the user to provide the URL.

Then register:
```bash
claude mcp add --transport http --scope <scope> <name> <url>/mcp
```

Always show the command before running it and ask for confirmation.

### C5: MCPs summary

Present a results table.

---

## Final Summary

After all parts complete, present a combined table:

```
| Resource         | Type   | Action                    |
|------------------|--------|---------------------------|
| my-skill         | skill  | Newly linked              |
| code-reviewer    | skill  | Already linked (skipped)  |
| my-agent         | agent  | Newly copied              |
| gemini-mcp       | MCP    | Registered (user scope)   |
| tunehop          | MCP    | Already registered (skip) |
```

---

## Phase 2: Cloud Account Sync (opt-in, skills only)

After the summary, ask: "Do you want to also check your Anthropic cloud account for skill discrepancies?"

If yes, read `references/cloud-sync.md` and follow the workflow there.

# anthropic-resource-packager

A Claude Code subagent that packages a Skill or MCP directory into the appropriate shareable artifact. Wraps the official Anthropic tooling (`package_skill.py`, `mcpb`) and never mutates the source directory.

## What it produces

| Source directory | Output |
|---|---|
| Claude Skill (`SKILL.md` present) | `<name>.skill` |
| Local stdio MCP (`manifest.json` / `bin` / `StdioServerTransport`) | `<name>.mcpb` |
| Remote/hosted MCP (`wrangler.toml` / hosted deploy scripts) | `<name>.mcpb` (stdio shim via `mcp-remote`) + `<name>-install.md` |

## How to invoke

Spawn via the `Agent` tool from a parent model:

```
Agent(
  subagent_type="anthropic-resource-packager",
  prompt="source_dir=/abs/path/to/my-skill"
)
```

**Parameters:**

- `source_dir` (required) — absolute path to the directory to package.
- `output_dir` (optional) — where to write artifacts. Defaults to `<source_dir>/../dist/`.
- `type_override` (optional, `skill | local-mcp | remote-mcp`) — bypass auto-detection when it misidentifies the type.
- `remote_url` (optional) — explicit deployed URL for remote MCPs. Agent attempts to extract from `wrangler.toml` or README if omitted.

## How it works

1. **Detect** — identifies the resource type from directory contents (SKILL.md, wrangler.toml, manifest.json, etc.) in strict priority order.
2. **Validate** — scans for secrets, checks required tooling is installed, and validates the skill/manifest before touching anything.
3. **Stage** — copies the source to a `mktemp -d` directory. All builds happen there; the original is never modified.
4. **Build** — shells out to `package_skill.py` (skills) or `mcpb pack` (MCPs). For Node MCPs, runs `npm ci --omit=dev` in the stage before packing so only production deps ship.
5. **Verify** — confirms each artifact exists, is non-empty, and is a valid zip before returning.

## Local environment setup

Two things are not committed to git and must be created after cloning: the `scripts/` symlinks and the `.venv/`. Both are required for skill packaging. MCP packaging (`mcpb`) has no local setup — see [Prerequisites](#prerequisites) below.

### 1. `scripts/` — packaging scripts

`package_skill.py` and `quick_validate.py` ship inside the `claude-plugins-official` plugin bundle. Symlink them into this agent's `scripts/` directory so the agent can invoke them without any path probing.

Find the source directory:

```bash
SCRIPTS_SRC=$(ls -d ~/.claude/plugins/marketplaces/claude-plugins-official/plugins/skill-creator/skills/skill-creator/scripts 2>/dev/null)
echo "$SCRIPTS_SRC"
```

If the path prints blank, the `skill-creator` plugin is not installed. Install it from the Claude Code plugin marketplace, then re-run the check.

Once confirmed, create the symlinks:

```bash
AGENT_DIR="$(pwd)"   # run from agents/anthropic-resource-packager/
SCRIPTS_SRC=~/.claude/plugins/marketplaces/claude-plugins-official/plugins/skill-creator/skills/skill-creator/scripts

mkdir -p "$AGENT_DIR/scripts"
ln -sf "$SCRIPTS_SRC/package_skill.py"  "$AGENT_DIR/scripts/package_skill.py"
ln -sf "$SCRIPTS_SRC/quick_validate.py" "$AGENT_DIR/scripts/quick_validate.py"
ln -sf "$SCRIPTS_SRC/__init__.py"       "$AGENT_DIR/scripts/__init__.py"
```

The symlinks track the plugin source, so they stay current when the plugin updates — no manual refresh needed.

### 2. `.venv/` — Python environment

`quick_validate.py` requires `pyyaml`, which is not in the macOS system Python. The agent uses a self-contained venv so it never touches the global Python environment.

```bash
# Run from agents/anthropic-resource-packager/
python3 -m venv .venv
.venv/bin/pip install pyyaml -q
```

Verify everything is wired up correctly by doing a dry run against any skill directory:

```bash
cd agents/anthropic-resource-packager
PYTHONPATH=. .venv/bin/python3 scripts/package_skill.py /path/to/any-skill /tmp/pkg-test
```

You should see `✅ Skill is valid!` followed by `✅ Successfully packaged skill to: /tmp/pkg-test/<name>.skill`. If the `.skill` file appears, the setup is complete.

### What to add to `.gitignore`

Both generated paths should be excluded:

```
.venv/
scripts/
```

> The `scripts/` directory contains only symlinks to files owned by the plugin bundle — committing them adds no value and the targets may not exist on other machines.

## Prerequisites

| Task | Requirement |
|---|---|
| Packaging skills | Local setup above (`scripts/` + `.venv/`) |
| Packaging local MCPs | `npm install -g @anthropic-ai/mcpb` |
| Packaging remote MCPs | same as local MCPs; also requires `npx` at runtime on the user's machine |

The agent will `BLOCKED:` with the exact install command if any MCP prerequisite is missing — it never auto-installs global packages.

## Known limitations

- **URL resolution is wrangler-centric.** Vercel/Fly/Deno-Deploy remote MCPs are detected correctly but URL extraction only works for Cloudflare Workers configs or URLs in the README. Others require passing `remote_url` explicitly.
- **Symlinks are machine-local.** The `scripts/` symlinks point to the plugin bundle path on the machine where setup was run. On a different machine the path is the same convention (`~/.claude/plugins/...`) but must be re-created after clone.
- **No artifact signing.** `mcpb sign` is not run. Unsigned bundles trigger a warning in Claude Desktop. Add signing once a cert is in place.
- **No smoke test.** The agent confirms the artifact is a valid zip but does not install or run it. A passing build does not guarantee a working bundle.

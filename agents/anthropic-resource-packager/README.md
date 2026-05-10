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

## Prerequisites

| Task | Requirement |
|---|---|
| Packaging skills | `python3` + `package_skill.py` (bundled with Warp, or set `ANTHROPIC_SKILLS_DIR`) |
| Packaging local MCPs | `npm install -g @anthropic-ai/mcpb` |
| Packaging remote MCPs | same as local MCPs; also requires `npx` at runtime on the user's machine |

The agent will `BLOCKED:` with the exact install command if any prerequisite is missing — it never auto-installs global packages.

## Known limitations

- **URL resolution is wrangler-centric.** Vercel/Fly/Deno-Deploy remote MCPs are detected correctly but URL extraction only works for Cloudflare Workers configs or URLs in the README. Others require passing `remote_url` explicitly.
- **`package_skill.py` lookup has a Warp-specific fallback.** The lookup path will rot if Warp restructures its bundle. Set `ANTHROPIC_SKILLS_DIR` to a stable location to avoid this.
- **No artifact signing.** `mcpb sign` is not run. Unsigned bundles trigger a warning in Claude Desktop. Add signing once a cert is in place.
- **No smoke test.** The agent confirms the artifact is a valid zip but does not install or run it. A passing build does not guarantee a working bundle.

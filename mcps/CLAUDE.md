# MCP Development Guidelines

## Overview
This directory contains Model Context Protocol (MCP) server implementations. Each subdirectory is a self-contained MCP server.

## Project Conventions

### Structure
Each MCP server lives in its own subdirectory (`mcps/<server-name>/`) and should follow this layout:
```
<server-name>/
  src/
    index.ts          # Entry point — registers and starts the server
    tools/            # One file per tool
    resources/        # One file per resource (if applicable)
    types.ts          # Shared types
  tests/
  package.json
  tsconfig.json
```

### Language & Runtime
- **TypeScript** — strict mode required (`"strict": true` in tsconfig). No `any`.
- **Node.js** — use the MCP SDK (`@modelcontextprotocol/sdk`).
- Runtime target: Node 20+.

### MCP SDK Usage
- Use `@modelcontextprotocol/sdk` — always check for the latest version before starting.
- Register tools via `server.tool()`, resources via `server.resource()`.
- Use `zod` for input schema validation on all tools.
- Transport: `StdioServerTransport` for local servers; `SSEServerTransport` for remote.

### Tool Design
- One tool per file under `src/tools/`. Export a registration function, not a class.
- Tool names: `snake_case`, descriptive, verb-first (e.g., `search_tracks`, `get_playlist`).
- Input schemas: validate with zod, keep fields minimal and explicit.
- Error responses: return structured `{ isError: true, content: [...] }` — never throw unhandled.
- Avoid side effects outside the tool's declared scope.

### Testing
- Unit tests for every tool handler.
- Mock external API clients at the boundary (not deep inside business logic).
- Integration tests where the tool makes real external calls — mark these with a `// integration` comment and skip by default in CI (`it.skip` or env guard).
- Test file mirrors source: `tests/tools/search_tracks.test.ts` for `src/tools/search_tracks.ts`.

### Dependencies
- Confirm before adding any new dependency.
- Prefer the Node.js stdlib and existing deps over pulling in new packages.
- Dev deps (types, test runners) are fine without confirmation.

### Secrets & Config
- All API keys, tokens, and credentials via environment variables — never hardcoded.
- Document required env vars in a comment block at the top of `src/index.ts`.
- Use a `.env.example` file (no real values) to document expected vars.

## Workflow Rules (mirrors global CLAUDE.md)
- Confirm before committing, pushing, or opening PRs.
- Confirm before installing packages.
- No `--no-verify` — fix hook failures at root.
- Flag adjacent issues; don't fix them unsolicited.

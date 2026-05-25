# gemini-mcp — Agent Instructions

Local stdio MCP server exposing Google Gemini transcription, chat, and model discovery.

## Setup (run in order — do not skip steps)

### 1. Install dependencies

```bash
npm install
```

Skip if `node_modules/` already exists and `package.json` has not changed.

### 2. Build

```bash
npm run build
```

This compiles `src/` → `dist/`. Always rebuild after any source change. Registering a stale `dist/` will silently run old code with no error.

### 3. Validate environment

Run this exact command — do NOT `cat`, `Read`, or otherwise load `.env` contents. The API key must never enter your context, transcript, or shell history.

```bash
grep -q '^GEMINI_API_KEY=.\+' .env && echo OK || echo MISSING
```

If the output is `MISSING` or the file does not exist: stop, tell the user to create `.env` from `.env.example` and populate `GEMINI_API_KEY`, and refer them to `README.md`. Do not proceed.

### 4. Register with Claude Code

Two options — pick one:

**Option A — `npm start` (use pre-built `dist/`):**
```bash
claude mcp add gemini-mcp -- npm start --prefix /Users/guy-ravid/Projects/ai/mcps/gemini-mcp
```
Use this when you've already run `npm run build` and want fast startup. Requires that `dist/` is up to date.

**Option B — `npm run start:build` (compile then start):**
```bash
claude mcp add gemini-mcp -- npm run start:build --prefix /Users/guy-ravid/Projects/ai/mcps/gemini-mcp
```
Use this when you want Claude Code to always compile before starting the server. Adds ~1-3s per spawn but eliminates stale-`dist/` issues.

Verify: `claude mcp list` should show `gemini-mcp`.

---

## Tools exposed

| Tool | Description |
|------|-------------|
| `list_models` | Returns all models available to the API key as JSON. Takes no inputs. Call this first to validate auth and discover valid model IDs. |
| `ask_gemini` | Text prompt → Gemini response. Optional `history` array for multi-turn. Optional `model` override. |
| `transcribe_media` | Upload a local audio/video file and get a transcript. Required: `filePath` (absolute path). Optional: `prompt`, `model`. |

Full parameter documentation is in `README.md`.

---

## Required env vars

- `GEMINI_API_KEY`

---

## Hard rules

- **Do not read `.env`.** Use `grep -q` for existence checks only. Never `cat`, `Read`, or interpolate the file.
- **Do not echo or log the API key.** It must not appear in any output, context, or history.
- **Do not commit `.env`.**
- **Do not skip the build step.** Stale `dist/` is silently wrong.
- **Do not bypass the env validation.** If the key is missing the server exits immediately with code 1 — debugging a silent startup failure is harder than catching it upfront.

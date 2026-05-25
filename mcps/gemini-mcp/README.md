# gemini-mcp

A local [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server that exposes Google Gemini capabilities — media transcription, text chat, and model discovery — to Claude Code and Claude Desktop over stdio. Built with `@google/genai` and the MCP SDK.

## Tools

### `transcribe_media`

Uploads a local audio or video file to Gemini's File API and returns a transcript. Video files are polled until Gemini finishes processing them before the transcript is requested.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `filePath` | string | yes | — | Absolute local path to the audio or video file |
| `prompt` | string | no | `"Provide a detailed transcript of this media file."` | Transcription instruction (e.g. `"Translate to Spanish"`, `"Summarize the meeting"`) |
| `model` | string | no | `gemini-2.5-flash` | Gemini model to use for generation |

Supported formats are listed in [`src/mime_types.ts`](src/mime_types.ts). Unknown extensions fall back to SDK auto-detection.

Upload and processing progress is logged to stderr. The polling loop has no timeout — very large video files may take a while.

### `ask_gemini`

Sends a text prompt to Gemini and returns the response. Supports optional conversation history for multi-turn follow-ups.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `prompt` | string | yes | — | Your question or instruction |
| `history` | array | no | — | Prior turns: `[{ "role": "user" | "model", "text": "..." }, ...]` |
| `model` | string | no | `gemini-2.5-flash` | Gemini model to use |

The `history` array is appended before the current `prompt`, allowing stateless multi-turn conversations.

### `list_models`

Returns all models available via your API key as a JSON array. Takes no inputs. Useful for discovering which model IDs are valid before overriding the default in `transcribe_media` or `ask_gemini`. The response includes non-text-generation models (embedding models, etc.).

---

## Requirements

- Node.js 20+
- A Gemini API key — get one from [Google AI Studio](https://aistudio.google.com/apikey)

## Setup

```bash
# 1. Install dependencies
npm install

# 2. Configure your API key
cp .env.example .env
# Edit .env and set GEMINI_API_KEY=<your key>

# 3. Build
npm run build

# 4. Register with Claude Code
# Use npm start (runs pre-built dist/):
claude mcp add gemini-mcp -- npm start --prefix /Users/guy-ravid/Projects/ai/mcps/gemini-mcp
# Or use start:build to compile before every server spawn:
claude mcp add gemini-mcp -- npm run start:build --prefix /Users/guy-ravid/Projects/ai/mcps/gemini-mcp
```

Verify the server is registered:

```bash
claude mcp list
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GEMINI_API_KEY` | yes | Google AI Studio API key. The server hard-exits at startup if this is missing (see `src/index.ts:17`). |

The key is loaded via `dotenv` from `.env` at startup. Never commit `.env` — it is gitignored.

## Development

Watch mode for iterative source changes:

```bash
npx tsc --watch
```

All server logs (upload progress, poll status, errors) go to stderr so they don't interfere with the MCP stdio transport. The registered Claude Code command points at `dist/index.js`, so rebuild (`npm run build`) after any source changes to pick them up.

## Project layout

```
src/
  index.ts              # Server entry point, tool dispatcher
  tools/
    transcribe_media.ts # File upload + transcript tool
    ask_gemini.ts       # Chat tool
    list_models.ts      # Model discovery tool
dist/                   # Compiled output (gitignored)
docs/
  walkthrough.md        # Initial setup notes
  implementation_plan.md
  BACKLOG.md            # Future work: Cloudflare Workers, Google OAuth multi-user
```

## Roadmap

See `docs/BACKLOG.md` for two documented future paths:
- **Cloudflare Workers + R2** — convert to a remote hosted MCP with cloud-side file storage
- **Google OAuth + KV** — multi-user support with per-user bearer token management

## Security

- `.env` is gitignored. Never commit it.
- The API key is read from the environment at startup and passed to `@google/genai` — it is never logged or returned in tool output.
- Uploaded files go to Google's File API under your API key's quota and are subject to Google's standard retention and deletion policies.

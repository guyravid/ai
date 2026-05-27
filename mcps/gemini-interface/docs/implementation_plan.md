# Gemini MCP Implementation Plan

We are building a new local Model Context Protocol (MCP) server that integrates directly with the Gemini API. 

## Goal Description
The `gemini-mcp` server will run locally on your machine via standard input/output (stdio), allowing it to securely read your local files. It will expose tools to transcribe local audio and video files using Gemini's native File API, as well as a generic tool to chat with Gemini.

## Proposed Architecture

1. **Hosting**: Local Node.js application (no Cloudflare Workers needed for the local version).
2. **Authentication**: Uses a `GEMINI_API_KEY` loaded from a local `.env` file.
3. **Media Handling**: The server will read local files (e.g., `/Users/guy/audio.mp3`), upload them temporarily to Google's GenAI File API, and pass the resulting `fileUri` to the model.

## Backlog Documentation (Future Improvements)
As requested, I will document the remote architecture paths in a `BACKLOG.md` file within the new project. This will explicitly outline:
- **Remote Media Uploads**: How to migrate the server to Cloudflare Workers using a dedicated `POST /upload` endpoint and Cloudflare R2 for temporary file storage.
- **Multi-User Google OAuth**: How to implement a full Google OAuth web flow, store tokens in Cloudflare KV, and map them to custom Bearer tokens for secure multi-user access on a single instance.

## Proposed Changes

### `gemini-mcp/` (New Subdirectory)

#### [NEW] `package.json` & `tsconfig.json`
- Standard Node.js + TypeScript setup (ESM modules).
- Dependencies: `@modelcontextprotocol/sdk`, `zod`, `@google/genai` (Google's official new SDK), `dotenv`.

#### [NEW] `src/index.ts`
- Entry point initializing the MCP server with `StdioServerTransport`.
- Loads `GEMINI_API_KEY` from `.env`.

#### [NEW] `src/tools/transcribe_media.ts`
- **Tool Name**: `transcribe_media`
- **Arguments**: 
  - `filePath` (string, required): Absolute local path to the audio or video file.
  - `prompt` (string, optional): Specific instructions (e.g., "Summarize this meeting" or "Translate this to Spanish").
- **Implementation**: Uses `google-genai` File API to upload the file, polls until processing is complete (required for videos), and generates content using `gemini-2.5-flash` or `gemini-1.5-pro`.

#### [NEW] `src/tools/ask_gemini.ts`
- **Tool Name**: `ask_gemini`
- **Arguments**:
  - `prompt` (string, required)
  - `history` (array, optional): Previous conversation context to allow follow-ups.
- **Implementation**: Passes the prompt and history to the Gemini API and returns the text response.

#### [NEW] `BACKLOG.md`
- Detailed architectural notes on migrating to Cloudflare + OAuth + R2.

---

## User Review Required
> [!IMPORTANT]
> - Do you have a preference between `gemini-2.5-flash` or `gemini-1.5-pro` as the default model for transcriptions? 
> - I will set up the tools to accept absolute file paths. This means when you use Claude Desktop/Code, you will simply provide the local path. Is this acceptable?

Please review this plan. If you approve, I will proceed with creating the directory, installing dependencies, and writing the code.

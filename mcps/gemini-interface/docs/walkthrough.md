# Gemini MCP Setup and Usage Walkthrough

I have successfully created and compiled the new `gemini-mcp` server. It runs locally using Node.js and TypeScript, taking full advantage of the `@google/genai` SDK for complex media uploads.

## Changes Made

- **Project Initialization**: Created `gemini-mcp` with `package.json`, `tsconfig.json`, and all required dependencies.
- **Tools Implemented**: 
  - `transcribe_media`: Automatically handles Google's File API for large audio/video files and polls until videos are processed before requesting a transcript from Gemini.
  - `ask_gemini`: Generic interface allowing chat and follow-ups with `gemini-2.5-flash` (or models of your choosing).
- **Architecture Backlog**: Documented the Cloudflare and Google OAuth alternative approaches in `BACKLOG.md` for easy reference later.
- **TypeScript Fix**: Resolved type safety issues during compilation. The code now successfully builds.

## How to Test and Run

To connect this local MCP server to Claude Code or Claude Desktop, follow these steps:

> [!IMPORTANT]
> **API Key Setup**: You must create a `.env` file inside `/Users/guy-ravid/Projects/ai/mcps/gemini-mcp` and populate it with your Gemini API key from Google AI Studio:
> `GEMINI_API_KEY="your-api-key-here"`

### 1. Connecting to Claude Code
Open your terminal and run:
```bash
claude mcp add gemini-mcp npx -y --prefix /Users/guy-ravid/Projects/ai/mcps/gemini-mcp ts-node src/index.ts
```
*(Note: Because we installed `ts-node`, you can run the source file directly, or you can point to `dist/index.js` after running `npx tsc`).*

### 2. Validating the Media Tool
Once connected, you can ask Claude to transcribe a file simply by providing an absolute path:
*"Can you transcribe the video located at `/Users/guy-ravid/Downloads/meeting.mp4` using the gemini-mcp?"*

Claude will automatically pass the local path to the MCP, which will handle the media upload and transcription behind the scenes.

## Next Steps
If you find yourself needing to share this tool with others or want to run it without local dependencies, you can revisit the `BACKLOG.md` to convert this to a Go binary or a Cloudflare Worker at any time!

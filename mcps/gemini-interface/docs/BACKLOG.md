# Architecture Backlog

This document captures alternative architectural paths discussed during the design phase for `gemini-mcp`, specifically focusing on deployment to Cloudflare Workers and implementing multi-user Google OAuth.

## 1. Cloudflare Workers + Remote File Uploads
Currently, `gemini-mcp` runs locally (via stdio) and reads files directly from the user's local disk. To deploy this to Cloudflare Workers for remote access:

### Constraints to Solve
The MCP protocol does not support native streaming of large binary files (like videos) within JSON tool calls. A remote Cloudflare Worker cannot access `/Users/...` on the client's machine.

### Proposed Architecture
1. **Host on Workers:** Update `index.ts` to use `@modelcontextprotocol/sdk/server/sse.js` instead of `stdio.js` and deploy via `wrangler`.
2. **Upload Endpoint:** Expose a standard HTTP POST endpoint on the Worker (e.g., `POST /upload`).
3. **Storage:** When a file is POSTed to `/upload`, the Worker buffers it or streams it directly to **Cloudflare R2** storage, returning a unique `fileID`.
4. **Tool Modification:** Update `transcribe_media` to accept a `fileID` or `publicURL` instead of a local `filePath`.
5. **Workflow:** 
   - User runs `curl -X POST -F "file=@video.mp4" https://gemini-mcp.workers.dev/upload` -> Gets `fileID: 123`.
   - User tells Claude: "Transcribe file 123".
   - MCP tool fetches from R2 and passes to Gemini File API.

## 2. Multi-User Authentication via Google OAuth
Currently, the MCP is secured for a single user via a local `.env` file containing the `GEMINI_API_KEY`. If deployed remotely for multiple users, sharing a single API key is not viable.

### Constraints to Solve
MCP clients (Claude) cannot natively render or handle OAuth web popups. 

### Proposed Architecture
1. **OAuth Flow:** 
   - The Cloudflare Worker exposes a `/login` endpoint.
   - Users visit `/login` in their browser, which redirects to Google's OAuth consent screen requesting Gemini/Vertex AI scopes.
   - Google redirects back to a `/callback` route on the Worker.
2. **Token Storage:** 
   - The Worker generates a secure, random `Bearer` token for the user.
   - The Worker stores the `Bearer` token as a key in **Cloudflare KV** or **Durable Objects**, with the value being the user's Google Access/Refresh tokens.
3. **MCP Connection:** 
   - The user configures Claude with their specific `Bearer` token: `claude mcp add ... -H "Authorization: Bearer <TOKEN>"`.
   - When the MCP is called, the Worker looks up the Google credentials in KV based on the Bearer header and initializes the `@google/genai` client using those OAuth credentials instead of a static API key.

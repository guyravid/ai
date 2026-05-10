/**
 * Required environment variables:
 *   ODESLI_USER_AGENT (optional) — custom User-Agent string sent to Odesli
 *
 * Durable Object bindings:
 *   MCP_OBJECT → TuneHopMCP (SQLite-backed, free-tier eligible)
 *
 * Routes:
 *   POST /mcp  — Streamable HTTP transport (MCP spec 2025-03-26)
 *   GET  /sse  — Legacy SSE transport (for older MCP clients)
 */

import { McpAgent } from "agents/mcp";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerConvertMusicLink } from "./tools/convert_music_link.js";

export interface Env {
  MCP_OBJECT: DurableObjectNamespace;
  ODESLI_USER_AGENT?: string;
  SPOTIFY_CLIENT_ID?: string;
  SPOTIFY_CLIENT_SECRET?: string;
}

export class TuneHopMCP extends McpAgent<Env> {
  server = new McpServer({ name: "tunehop", version: "0.1.0" });

  async init(): Promise<void> {
    const spotify =
      this.env.SPOTIFY_CLIENT_ID && this.env.SPOTIFY_CLIENT_SECRET
        ? { clientId: this.env.SPOTIFY_CLIENT_ID, clientSecret: this.env.SPOTIFY_CLIENT_SECRET }
        : undefined;
    registerConvertMusicLink(this.server, { userAgent: this.env.ODESLI_USER_AGENT, spotify });
  }
}

export default {
  async fetch(
    request: Request,
    env: Env,
    ctx: ExecutionContext
  ): Promise<Response> {
    const { pathname } = new URL(request.url);

    if (pathname.startsWith("/sse")) {
      return TuneHopMCP.serveSSE("/sse").fetch(request, env, ctx);
    }
    if (pathname.startsWith("/mcp")) {
      return TuneHopMCP.serve("/mcp").fetch(request, env, ctx);
    }

    return new Response("Not found", { status: 404 });
  },
};

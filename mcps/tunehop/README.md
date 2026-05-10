# tunehop

MCP server that converts a music URL from any platform to equivalent links on all other platforms, via the [Odesli (song.link)](https://odesli.co) API.

Built on Cloudflare Workers + Durable Objects. Exposes two transports:
- `POST /mcp` — Streamable HTTP (MCP spec 2025-03-26, preferred)
- `GET  /sse` — Legacy SSE (for older MCP clients)

## Tool

### `convert_music_link`

| Input | Type | Required |
|-------|------|----------|
| `url` | `string` (URL) | Yes |
| `platforms` | `string[]` | No — omit to return all |

**Output:**
```json
{
  "title": "Midnight Rain",
  "artistName": "Taylor Swift",
  "albumName": "Midnights",
  "type": "song",
  "links": {
    "spotify": "https://open.spotify.com/track/...",
    "appleMusic": "https://music.apple.com/...",
    "youtube": "https://youtube.com/watch?v=..."
  },
  "universalLink": "https://song.link/s/...",
  "matchConfidence": "exact"
}
```

`matchConfidence` is `exact` when all platforms agree on the title and artist, `closest` when Odesli made a best-guess match. Platform keys come from Odesli — the list is not hardcoded.

Unauthenticated Odesli usage is rate-limited to ~10 req/min. 429 responses are returned as structured MCP errors.

## Local dev

```sh
npm install
npx wrangler dev
# Streamable HTTP: http://localhost:8787/mcp
# Legacy SSE:      http://localhost:8787/sse
```

## Deploy

```sh
npx wrangler login
npx wrangler deploy
# → https://tunehop.<your-subdomain>.workers.dev
```

After deploying, run once to generate typed bindings:
```sh
npx wrangler types
```

## Connect to Claude Code

```sh
# Streamable HTTP (preferred)
claude mcp add --transport http tunehop https://tunehop.<your-subdomain>.workers.dev/mcp

# Legacy SSE
claude mcp add --transport sse tunehop https://tunehop.<your-subdomain>.workers.dev/sse
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ODESLI_USER_AGENT` | `tunehop-mcp/0.1 (...)` | User-Agent sent to Odesli |

Set via `.dev.vars` locally or `wrangler secret put ODESLI_USER_AGENT` in production.

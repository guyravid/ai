# music-link-converter

A Claude Code skill that converts music links between Apple Music and Spotify (and vice versa), then renders a formatted card with both platform URLs and a universal song.link page.

Works for songs and albums. Playlists are not supported.

## Trigger phrases

- "Find this on Spotify"
- "Get the Apple Music link"
- "Convert this track link"
- Pasting any `music.apple.com` or `open.spotify.com` URL

## Dependency: tunehop MCP (required)

> **The tunehop MCP is the primary engine of this skill.** Without it, the skill falls back to web search and scraping, which is slower, less reliable, and subject to rate limits or bot blocking. You should deploy and connect the MCP before using this skill in production.

The MCP wraps the [Odesli (song.link)](https://odesli.co) API and exposes a single `convert_music_link` tool that resolves a URL to all known platform equivalents in one call.

### 1. Deploy the MCP

Clone or navigate to the `mcps/tunehop` directory and deploy to Cloudflare Workers:

```sh
npm install
npx wrangler login
npx wrangler deploy
# Deployed to: https://tunehop.<your-subdomain>.workers.dev
```

> You need a free [Cloudflare account](https://cloudflare.com) and `wrangler` installed (`npm i -g wrangler`).

### 2. Connect to Claude Code

```sh
# Streamable HTTP (preferred)
claude mcp add --transport http tunehop https://tunehop.<your-subdomain>.workers.dev/mcp

# Legacy SSE (for older clients)
claude mcp add --transport sse tunehop https://tunehop.<your-subdomain>.workers.dev/sse
```

Verify the connection:

```sh
claude mcp list
```

`tunehop` should appear in the list with a connected status.

## Install the skill

Copy `SKILL.md` into your Claude Code skills directory:

```sh
cp SKILL.md ~/.claude/skills/music-link-converter.md
```

Or symlink it if you're actively developing:

```sh
ln -s "$(pwd)/SKILL.md" ~/.claude/skills/music-link-converter.md
```

## Output format

```
🎵  Midnight Rain
👤  Taylor Swift
💿  Midnights
📀  Song

──────────────────────────────
🍎  Apple Music
    https://music.apple.com/...

🟢  Spotify
    https://open.spotify.com/track/...

🔗  Universal link
    https://song.link/s/...
──────────────────────────────
```

If Odesli made a best-guess match rather than an exact one, a warning is appended:

```
⚠️  Closest match — metadata may differ from the original.
```

## Fallback behavior (no MCP)

If the tunehop MCP is unavailable or returns no result, the skill degrades gracefully:

1. Fetches the source URL to extract title and artist metadata
2. Searches for the song.link page via web search
3. Falls back to a direct platform search if song.link isn't indexed

The universal link row is omitted when the direct fallback is used.

## Rate limits

The unauthenticated Odesli API is limited to ~10 requests/min. The MCP surfaces 429 errors as structured MCP errors. If you hit limits frequently, consider adding an Odesli API key via `wrangler secret put ODESLI_API_KEY`.

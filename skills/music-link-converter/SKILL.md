---
name: music-link-converter
description: Convert music links between Apple Music and Spotify (and vice versa). Use this skill whenever a user shares a music.apple.com or open.spotify.com URL and wants the equivalent on the other platform. Trigger on phrases like "find this on Spotify", "Apple Music link", "convert this song link", "get the Spotify version", "share this on Apple Music", or any time a user pastes a Spotify or Apple Music URL and wants the equivalent. Works for songs and albums; playlists are not supported.
---

# Music Link Converter

Convert an Apple Music link to Spotify (or vice versa) and return a richly formatted card.

---

## Step 1 — Detect platform

Inspect the URL to determine the source platform:
- `music.apple.com` → Apple Music
- `open.spotify.com` → Spotify

If neither pattern matches, tell the user the URL isn't recognized and ask for a valid Apple Music or Spotify link. Stop here.

If the URL looks like a playlist (contains `/playlist/`), inform the user that playlist conversion isn't supported and offer to convert individual tracks instead.

---

## Step 2 — Try the tunehop MCP (highest priority)

Call `mcp__tunehop__convert_music_link` with the user's URL:

```
url: <user's URL>
platforms: ["appleMusic", "spotify"]
```

**If the call succeeds**, the response is a `ConvertMusicLinkOutput` with these fields:
- `title`, `artistName`, `albumName`, `type` — track/album metadata
- `links` — object keyed by platform (e.g. `{ appleMusic: "...", spotify: "..." }`)
- `universalLink` — the song.link page URL
- `matchConfidence` — `"exact"` or `"closest"`

Map these directly to the card fields and **skip to Step 7**. No web fetching needed.

**If the call fails** (network error, MCP unavailable, or the URL is not found in Odesli), fall through to Step 3.

---

## Step 3 — Get metadata from the source URL

`web_fetch` the user's original URL (it came from them, so it's always allowed). Parse the page to extract:
- `title` — song or album name
- `artistName`
- `albumName` (if available)
- `type` — song or album

This is the ground truth for the match quality comparison later.

**If `web_fetch` fails** (e.g. blocked by robots.txt, as Apple Music commonly does):
- Extract the `title` from the URL slug (e.g. `wano-theme-drums-of-liberation` → "Wano Theme (Drums of Liberation)")
- Ask the user for the artist name directly before proceeding:
  > "Who's the artist? There are often many covers of the same song, so I want to make sure I find the right one."
- Wait for their reply, then continue with the confirmed artist name.

---

## Step 4 — Look up via song.link (primary fallback)

Search for the song.link page using `web_search`:

```
song.link "{title}" "{artistName}"
```

Scan the results for a URL matching `song.link/...` or `odesli.co/...`. If found, `web_fetch` that page and extract:
- The Apple Music link (look for `music.apple.com` in the page)
- The Spotify link (look for `open.spotify.com/track` or `open.spotify.com/album`)
- Use the song.link page URL itself as the universal link

If the song.link page is found and both platform links are present → proceed to Step 7.

---

## Step 5 — Fallback: direct platform search

If no song.link page was found, or if the target platform link is missing from it, fall back to a direct search:

```
"{title}" "{artistName}" {target platform}
```

Where `{target platform}` is `spotify` or `apple music`.

Pick the most relevant result — prefer an exact title + artist match over a partial one. If the best result differs in title or artist name, flag it as a closest match.

The universal link is not available in fallback mode — omit that row from the card.

---

## Step 6 — Assess match quality

*(Skip this step when using MCP results — `matchConfidence` is already provided.)*

Compare the target result's title and artist against the source metadata from Step 3.

Flag as a **closest match** (⚠️ warning) if:
- The title differs by more than minor punctuation or capitalization
- The artist name differs (e.g. featuring credits stripped or changed)

A clean match requires no warning.

---

## Step 7 — Render the output card

Output a single fenced code block (no language tag) with this layout:

```
🎵  {title}
👤  {artistName}
💿  {albumName}             ← omit if type is album
📀  {type: Song | Album}

──────────────────────────────
🍎  Apple Music
    {apple music URL}

🟢  Spotify
    {spotify URL}

🔗  Universal link
    {song.link page URL}    ← omit if direct platform fallback was used
──────────────────────────────
```

- If one platform link is missing, show `(not available)` for that section.
- If this is a closest match (`matchConfidence === "closest"` from MCP, or flagged in Step 6), append after the closing line:
```
⚠️  Closest match — metadata may differ from the original.
```

- Keep the separator line exactly 30 `─` characters for visual consistency.

---

## Notes

- The tunehop MCP is the preferred path — it's faster and more reliable than web scraping. Only fall through to Steps 3–6 when it's unavailable or returns no result.
- song.link pages are not always indexed by search engines — the web fallback is expected to fire occasionally and is not an error.
- The MCP and Odesli API may return links for many platforms beyond Apple Music and Spotify. Ignore those unless the user asks.
- If both the MCP and all fallbacks fail to find the target platform, render the card with `(not available)` for that platform and suggest the user try song.link manually.

import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { fetchOdesli } from "../odesli.js";
import { getSpotifyToken, searchSpotifyTrack, type SpotifyCredentials } from "../spotify.js";
import type {
  OdesliResponse,
  ConvertMusicLinkOutput,
  MatchConfidence,
} from "../types.js";

const DEFAULT_USER_AGENT =
  "tunehop-mcp/0.1 (+https://github.com/guyravid/tunehop)";

const InputSchema = {
  url: z.string().url().describe("Music URL from any Odesli-supported platform"),
  platforms: z
    .array(z.string())
    .optional()
    .describe(
      "Filter to these platform keys (e.g. spotify, appleMusic). Omit for all."
    ),
};

// Normalise for cross-entity comparison: strip feature credits, remaster/remix tags, and whitespace
function norm(s: string | undefined | null): string {
  if (!s) return "";
  return s
    .toLowerCase()
    .trim()
    .replace(/\s+/g, " ")
    .replace(/\s*[\(\[]feat\.?[^\)\]]*[\)\]]/gi, "")
    .replace(/\s*[\(\[]\s*(remaster(ed)?|remix|live|acoustic|radio edit|single version)[^\)\]]*[\)\]]/gi, "")
    .replace(/\s*-\s*(remaster(ed)?|remix|live|acoustic|radio edit|single version).*$/i, "");
}

function errorResult(message: string) {
  return { isError: true as const, content: [{ type: "text" as const, text: message }] };
}

// Exported for testing
export function buildOutput(
  data: OdesliResponse,
  platforms?: string[]
): ConvertMusicLinkOutput | null {
  const primary = data.entitiesByUniqueId[data.entityUniqueId];
  if (!primary) return null;

  const entities = Object.values(data.entitiesByUniqueId);
  const allMatch = entities.every(
    (e) =>
      norm(e.title) === norm(primary.title) &&
      norm(e.artistName) === norm(primary.artistName)
  );
  const matchConfidence: MatchConfidence = allMatch ? "exact" : "closest";

  let links: Record<string, string> = Object.fromEntries(
    Object.entries(data.linksByPlatform).map(([k, v]) => [k, v.url])
  );
  if (platforms && platforms.length > 0) {
    links = Object.fromEntries(
      platforms.filter((p) => p in links).map((p) => [p, links[p]])
    );
  }

  return {
    title: primary.title ?? null,
    artistName: primary.artistName ?? null,
    albumName: primary.albumName ?? null,
    type: primary.type,
    links,
    universalLink: data.pageUrl,
    matchConfidence,
  };
}

export interface RegisterOptions {
  userAgent?: string;
  spotify?: SpotifyCredentials;
}

type HandlerResult = {
  isError?: true;
  content: Array<{ type: "text"; text: string }>;
  structuredContent?: Record<string, unknown>;
};

// Exported for testing — contains all fetch + transform logic
export async function handleConvertMusicLink(
  args: { url: string; platforms?: string[] },
  userAgent: string,
  spotify?: SpotifyCredentials
): Promise<HandlerResult> {
  const response = await fetchOdesli(args.url, userAgent);

  if (response.status === 429) {
    return errorResult(
      "Odesli rate limit reached (10 req/min unauthenticated). Retry shortly."
    );
  }
  if (!response.ok) {
    const body = await response.text().catch(() => "");
    return errorResult(`Odesli API error ${response.status}: ${body}`);
  }

  const data = (await response.json()) as OdesliResponse;

  // Spotify fallback: if Odesli has no Spotify link and credentials are available,
  // do a single GET search by title + artist to fill the gap.
  const wantsSpotify = !args.platforms || args.platforms.includes("spotify");
  if (wantsSpotify && !data.linksByPlatform["spotify"] && spotify) {
    const primary = data.entitiesByUniqueId[data.entityUniqueId];
    if (primary?.title && primary?.artistName) {
      try {
        const token = await getSpotifyToken(spotify);
        const spotifyUrl = await searchSpotifyTrack(primary.title, primary.artistName, token);
        if (spotifyUrl) {
          data.linksByPlatform["spotify"] = { url: spotifyUrl, entityUniqueId: "" };
        }
      } catch {
        // Spotify fallback failure is non-fatal — return what Odesli gave us
      }
    }
  }

  const out = buildOutput(data, args.platforms);

  if (!out) {
    return errorResult("Odesli returned no canonical entity for this URL.");
  }

  return {
    content: [{ type: "text", text: JSON.stringify(out, null, 2) }],
    structuredContent: out as unknown as Record<string, unknown>,
  };
}

export function registerConvertMusicLink(
  server: McpServer,
  { userAgent = DEFAULT_USER_AGENT, spotify }: RegisterOptions = {}
): void {
  server.registerTool(
    "convert_music_link",
    {
      title: "Convert music link",
      description:
        "Convert a music URL from any platform to equivalent links on all other platforms via the Odesli (song.link) API.",
      inputSchema: InputSchema,
    },
    (args) => handleConvertMusicLink(args, userAgent, spotify)
  );
}

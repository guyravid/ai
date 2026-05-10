// Only two calls are made to Spotify:
//   1. POST /api/token  — client credentials auth, not a data query
//   2. GET  /v1/search  — read-only track lookup by title + artist

const TOKEN_ENDPOINT = "https://accounts.spotify.com/api/token";
const SEARCH_ENDPOINT = "https://api.spotify.com/v1/search";

export interface SpotifyCredentials {
  clientId: string;
  clientSecret: string;
}

interface SpotifyTokenResponse {
  access_token: string;
}

interface SpotifySearchResponse {
  tracks?: {
    items?: Array<{
      external_urls?: { spotify?: string };
    }>;
  };
}

export async function getSpotifyToken(
  credentials: SpotifyCredentials,
  fetcher: typeof fetch = fetch
): Promise<string> {
  const basicAuth = btoa(`${credentials.clientId}:${credentials.clientSecret}`);
  const response = await fetcher(TOKEN_ENDPOINT, {
    method: "POST",
    headers: {
      Authorization: `Basic ${basicAuth}`,
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: "grant_type=client_credentials",
  });
  if (!response.ok) {
    throw new Error(`Spotify token fetch failed: ${response.status}`);
  }
  const data = (await response.json()) as SpotifyTokenResponse;
  return data.access_token;
}

export async function searchSpotifyTrack(
  title: string,
  artistName: string,
  token: string,
  fetcher: typeof fetch = fetch
): Promise<string | null> {
  const q = `track:"${title}" artist:"${artistName}"`;
  const url = `${SEARCH_ENDPOINT}?q=${encodeURIComponent(q)}&type=track&limit=1`;
  const response = await fetcher(url, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) return null;
  const data = (await response.json()) as SpotifySearchResponse;
  return data.tracks?.items?.[0]?.external_urls?.spotify ?? null;
}

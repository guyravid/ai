import { describe, it, expect, vi, beforeEach } from "vitest";
import { getSpotifyToken, searchSpotifyTrack } from "../src/spotify.js";

const CREDS = { clientId: "test-id", clientSecret: "test-secret" };
const TOKEN = "test-access-token";

describe("getSpotifyToken", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("sends a POST with Basic auth and client_credentials grant", async () => {
    const mockFetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ access_token: TOKEN }), { status: 200 })
    );
    await getSpotifyToken(CREDS, mockFetch);

    const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://accounts.spotify.com/api/token");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>)["Authorization"]).toMatch(/^Basic /);
    expect(init.body).toBe("grant_type=client_credentials");
  });

  it("returns the access_token from the response", async () => {
    const mockFetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ access_token: TOKEN }), { status: 200 })
    );
    const result = await getSpotifyToken(CREDS, mockFetch);
    expect(result).toBe(TOKEN);
  });

  it("throws on non-2xx response", async () => {
    const mockFetch = vi.fn().mockResolvedValue(new Response("Unauthorized", { status: 401 }));
    await expect(getSpotifyToken(CREDS, mockFetch)).rejects.toThrow("401");
  });
});

describe("searchSpotifyTrack", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("makes a GET request to the search endpoint with Authorization header", async () => {
    const mockFetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ tracks: { items: [] } }), { status: 200 })
    );
    await searchSpotifyTrack("Never Gonna Give You Up", "Rick Astley", TOKEN, mockFetch);

    const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("https://api.spotify.com/v1/search");
    expect(url).toContain("type=track");
    expect(url).toContain("limit=1");
    expect((init as RequestInit & { method?: string }).method).toBeUndefined(); // GET by default
    expect((init.headers as Record<string, string>)["Authorization"]).toBe(`Bearer ${TOKEN}`);
  });

  it("returns the Spotify URL from the first result", async () => {
    const spotifyUrl = "https://open.spotify.com/track/abc123";
    const mockFetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ tracks: { items: [{ external_urls: { spotify: spotifyUrl } }] } }),
        { status: 200 }
      )
    );
    const result = await searchSpotifyTrack("Title", "Artist", TOKEN, mockFetch);
    expect(result).toBe(spotifyUrl);
  });

  it("returns null when no results", async () => {
    const mockFetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ tracks: { items: [] } }), { status: 200 })
    );
    const result = await searchSpotifyTrack("Title", "Artist", TOKEN, mockFetch);
    expect(result).toBeNull();
  });

  it("returns null on non-2xx response instead of throwing", async () => {
    const mockFetch = vi.fn().mockResolvedValue(new Response("error", { status: 401 }));
    const result = await searchSpotifyTrack("Title", "Artist", TOKEN, mockFetch);
    expect(result).toBeNull();
  });
});

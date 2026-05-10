import { describe, it, expect, vi, beforeEach } from "vitest";
import { fetchOdesli } from "../src/odesli.js";

const MOCK_URL = "https://open.spotify.com/track/abc123";
const DEFAULT_UA = "tunehop-mcp/0.1 (+https://github.com/guyravid/tunehop)";

describe("fetchOdesli", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("encodes the URL and appends userCountry=CA", async () => {
    const mockFetch = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));

    await fetchOdesli(MOCK_URL, DEFAULT_UA, mockFetch);

    const [calledUrl] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(calledUrl).toContain(`url=${encodeURIComponent(MOCK_URL)}`);
    expect(calledUrl).toContain("userCountry=CA");
  });

  it("sends the provided User-Agent header", async () => {
    const mockFetch = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));
    const customUA = "my-custom-ua/1.0";

    await fetchOdesli(MOCK_URL, customUA, mockFetch);

    const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>)["User-Agent"]).toBe(customUA);
  });

  it("returns the raw Response untransformed", async () => {
    const stub = new Response(JSON.stringify({ pageUrl: "https://song.link/x" }), {
      status: 200,
    });
    const mockFetch = vi.fn().mockResolvedValue(stub);

    const result = await fetchOdesli(MOCK_URL, DEFAULT_UA, mockFetch);

    expect(result).toBe(stub);
  });

  it("passes through non-2xx responses without throwing", async () => {
    const mockFetch = vi.fn().mockResolvedValue(new Response("Not found", { status: 404 }));

    const result = await fetchOdesli(MOCK_URL, DEFAULT_UA, mockFetch);

    expect(result.status).toBe(404);
  });
});

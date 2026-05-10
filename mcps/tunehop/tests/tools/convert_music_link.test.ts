import { describe, it, expect, vi, beforeEach } from "vitest";
import { buildOutput, handleConvertMusicLink } from "../../src/tools/convert_music_link.js";
import type { OdesliResponse } from "../../src/types.js";

vi.mock("../../src/odesli.js", () => ({ fetchOdesli: vi.fn() }));
vi.mock("../../src/spotify.js", () => ({
  getSpotifyToken: vi.fn(),
  searchSpotifyTrack: vi.fn(),
}));

import { fetchOdesli } from "../../src/odesli.js";
import { getSpotifyToken, searchSpotifyTrack } from "../../src/spotify.js";

// Minimal fixture representing a Spotify → multi-platform Odesli response
function makeFixture(overrides?: Partial<OdesliResponse>): OdesliResponse {
  return {
    entityUniqueId: "SPOTIFY_SONG::abc",
    userCountry: "CA",
    pageUrl: "https://song.link/s/abc",
    entitiesByUniqueId: {
      "SPOTIFY_SONG::abc": {
        id: "abc",
        type: "song",
        title: "Midnight Rain",
        artistName: "Taylor Swift",
        albumName: "Midnights",
        apiProvider: "spotify",
        platforms: ["spotify"],
      },
      "ITUNES_SONG::123": {
        id: "123",
        type: "song",
        title: "Midnight Rain",
        artistName: "Taylor Swift",
        albumName: "Midnights",
        apiProvider: "itunes",
        platforms: ["appleMusic"],
      },
    },
    linksByPlatform: {
      spotify: { url: "https://open.spotify.com/track/abc", entityUniqueId: "SPOTIFY_SONG::abc" },
      appleMusic: { url: "https://music.apple.com/track/123", entityUniqueId: "ITUNES_SONG::123" },
      youtube: { url: "https://youtube.com/watch?v=xyz", entityUniqueId: "YOUTUBE_VIDEO::xyz" },
    },
    ...overrides,
  };
}

describe("buildOutput", () => {
  it("returns null when canonical entity is missing", () => {
    const data = makeFixture({ entityUniqueId: "MISSING::999" });
    expect(buildOutput(data)).toBeNull();
  });

  it("maps all platform links by default", () => {
    const out = buildOutput(makeFixture());
    expect(out).not.toBeNull();
    expect(out!.links).toEqual({
      spotify: "https://open.spotify.com/track/abc",
      appleMusic: "https://music.apple.com/track/123",
      youtube: "https://youtube.com/watch?v=xyz",
    });
  });

  it("filters links when platforms is provided", () => {
    const out = buildOutput(makeFixture(), ["spotify", "youtube"]);
    expect(out!.links).toEqual({
      spotify: "https://open.spotify.com/track/abc",
      youtube: "https://youtube.com/watch?v=xyz",
    });
  });

  it("silently drops platform keys not in the Odesli response", () => {
    const out = buildOutput(makeFixture(), ["spotify", "tidal"]);
    expect(Object.keys(out!.links)).toEqual(["spotify"]);
  });

  it("sets matchConfidence to exact when all entities share the same title and artist", () => {
    const out = buildOutput(makeFixture());
    expect(out!.matchConfidence).toBe("exact");
  });

  it("normalises away remaster tags in parentheses before comparing → exact", () => {
    const data = makeFixture();
    data.entitiesByUniqueId["ITUNES_SONG::123"].title = "Midnight Rain (Remastered)";
    const out = buildOutput(data);
    expect(out!.matchConfidence).toBe("exact");
  });

  it("normalises away dash-separated remaster tags before comparing → exact", () => {
    const data = makeFixture();
    data.entitiesByUniqueId["ITUNES_SONG::123"].title = "Midnight Rain - Remastered 2024";
    const out = buildOutput(data);
    expect(out!.matchConfidence).toBe("exact");
  });

  it("sets matchConfidence to closest for genuinely divergent titles", () => {
    const data = makeFixture();
    data.entitiesByUniqueId["ITUNES_SONG::123"].title = "A Completely Different Song";
    const out = buildOutput(data);
    expect(out!.matchConfidence).toBe("closest");
  });

  it("treats missing title as non-matching → closest", () => {
    const data = makeFixture();
    delete data.entitiesByUniqueId["ITUNES_SONG::123"].title;
    const out = buildOutput(data);
    expect(out!.matchConfidence).toBe("closest");
  });

  it("includes canonical metadata from the primary entity", () => {
    const out = buildOutput(makeFixture());
    expect(out).toMatchObject({
      title: "Midnight Rain",
      artistName: "Taylor Swift",
      albumName: "Midnights",
      type: "song",
      universalLink: "https://song.link/s/abc",
    });
  });

  it("sets albumName to null when absent", () => {
    const data = makeFixture();
    delete data.entitiesByUniqueId["SPOTIFY_SONG::abc"].albumName;
    const out = buildOutput(data);
    expect(out!.albumName).toBeNull();
  });
});

describe("handleConvertMusicLink", () => {
  const UA = "test-ua/0.0";
  const SPOTIFY_URL = "https://open.spotify.com/track/abc";

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns isError on 429", async () => {
    vi.mocked(fetchOdesli).mockResolvedValueOnce(new Response("rate limited", { status: 429 }));
    const result = await handleConvertMusicLink({ url: SPOTIFY_URL }, UA);
    expect(result.isError).toBe(true);
    expect(result.content[0].text).toMatch(/rate limit/i);
  });

  it("returns isError on 500 with body text", async () => {
    vi.mocked(fetchOdesli).mockResolvedValueOnce(new Response("internal error", { status: 500 }));
    const result = await handleConvertMusicLink({ url: SPOTIFY_URL }, UA);
    expect(result.isError).toBe(true);
    expect(result.content[0].text).toMatch(/500/);
    expect(result.content[0].text).toMatch(/internal error/);
  });

  it("returns structured output on a successful response", async () => {
    vi.mocked(fetchOdesli).mockResolvedValueOnce(
      new Response(JSON.stringify(makeFixture()), { status: 200 })
    );
    const result = await handleConvertMusicLink({ url: SPOTIFY_URL }, UA);
    expect(result.isError).toBeUndefined();
    const parsed = JSON.parse(result.content[0].text);
    expect(parsed.title).toBe("Midnight Rain");
    expect(parsed.universalLink).toBe("https://song.link/s/abc");
    expect(result.structuredContent).toMatchObject({ title: "Midnight Rain" });
  });

  it("filters platforms in the returned output", async () => {
    vi.mocked(fetchOdesli).mockResolvedValueOnce(
      new Response(JSON.stringify(makeFixture()), { status: 200 })
    );
    const result = await handleConvertMusicLink({ url: SPOTIFY_URL, platforms: ["spotify"] }, UA);
    const parsed = JSON.parse(result.content[0].text);
    expect(Object.keys(parsed.links)).toEqual(["spotify"]);
  });

  it("returns isError when Odesli response has no canonical entity", async () => {
    const broken = makeFixture({ entityUniqueId: "MISSING::0" });
    vi.mocked(fetchOdesli).mockResolvedValueOnce(
      new Response(JSON.stringify(broken), { status: 200 })
    );
    const result = await handleConvertMusicLink({ url: SPOTIFY_URL }, UA);
    expect(result.isError).toBe(true);
    expect(result.content[0].text).toMatch(/no canonical entity/i);
  });

  describe("Spotify fallback", () => {
    const SPOTIFY_CREDS = { clientId: "id", clientSecret: "secret" };
    const FOUND_URL = "https://open.spotify.com/track/found123";

    function makeNoSpotifyFixture() {
      const f = makeFixture();
      delete f.linksByPlatform["spotify"];
      return f;
    }

    it("adds a Spotify link when Odesli has none and credentials are provided", async () => {
      vi.mocked(fetchOdesli).mockResolvedValueOnce(
        new Response(JSON.stringify(makeNoSpotifyFixture()), { status: 200 })
      );
      vi.mocked(getSpotifyToken).mockResolvedValueOnce("tok");
      vi.mocked(searchSpotifyTrack).mockResolvedValueOnce(FOUND_URL);

      const result = await handleConvertMusicLink({ url: "https://music.apple.com/track/1" }, UA, SPOTIFY_CREDS);
      const parsed = JSON.parse(result.content[0].text);
      expect(parsed.links.spotify).toBe(FOUND_URL);
    });

    it("does not call Spotify when Odesli already has a Spotify link", async () => {
      vi.mocked(fetchOdesli).mockResolvedValueOnce(
        new Response(JSON.stringify(makeFixture()), { status: 200 })
      );

      await handleConvertMusicLink({ url: "https://music.apple.com/track/1" }, UA, SPOTIFY_CREDS);
      expect(getSpotifyToken).not.toHaveBeenCalled();
    });

    it("does not call Spotify when no credentials are provided", async () => {
      vi.mocked(fetchOdesli).mockResolvedValueOnce(
        new Response(JSON.stringify(makeNoSpotifyFixture()), { status: 200 })
      );

      await handleConvertMusicLink({ url: "https://music.apple.com/track/1" }, UA);
      expect(getSpotifyToken).not.toHaveBeenCalled();
    });

    it("does not call Spotify when platforms filter excludes spotify", async () => {
      vi.mocked(fetchOdesli).mockResolvedValueOnce(
        new Response(JSON.stringify(makeNoSpotifyFixture()), { status: 200 })
      );

      await handleConvertMusicLink({ url: "https://music.apple.com/track/1", platforms: ["appleMusic"] }, UA, SPOTIFY_CREDS);
      expect(getSpotifyToken).not.toHaveBeenCalled();
    });

    it("returns result without spotify link if search finds nothing", async () => {
      vi.mocked(fetchOdesli).mockResolvedValueOnce(
        new Response(JSON.stringify(makeNoSpotifyFixture()), { status: 200 })
      );
      vi.mocked(getSpotifyToken).mockResolvedValueOnce("tok");
      vi.mocked(searchSpotifyTrack).mockResolvedValueOnce(null);

      const result = await handleConvertMusicLink({ url: "https://music.apple.com/track/1" }, UA, SPOTIFY_CREDS);
      const parsed = JSON.parse(result.content[0].text);
      expect(parsed.links.spotify).toBeUndefined();
      expect(result.isError).toBeUndefined();
    });

    it("returns result without spotify link if token fetch throws (non-fatal)", async () => {
      vi.mocked(fetchOdesli).mockResolvedValueOnce(
        new Response(JSON.stringify(makeNoSpotifyFixture()), { status: 200 })
      );
      vi.mocked(getSpotifyToken).mockRejectedValueOnce(new Error("auth failed"));

      const result = await handleConvertMusicLink({ url: "https://music.apple.com/track/1" }, UA, SPOTIFY_CREDS);
      expect(result.isError).toBeUndefined();
      const parsed = JSON.parse(result.content[0].text);
      expect(parsed.links.spotify).toBeUndefined();
    });
  });

  // integration
  it.skip("calls real Odesli with a Spotify URL and returns links", async () => {
    const { fetchOdesli: real } = await vi.importActual<typeof import("../../src/odesli.js")>("../../src/odesli.js");
    vi.mocked(fetchOdesli).mockImplementationOnce(real);
    const result = await handleConvertMusicLink(
      { url: "https://open.spotify.com/track/4cOdK2wGLETKBW3PvgPWqT" },
      "tunehop-test/0.1"
    );
    console.log(result);
    expect(result.isError).toBeUndefined();
  });
});

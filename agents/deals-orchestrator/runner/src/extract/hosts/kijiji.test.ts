import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { describe, it } from "node:test";
import { extractPage } from "../extract.ts";
import { extractKijiji } from "./kijiji.ts";

const PAGE_URL = "https://www.kijiji.ca/b-ottawa/mountain-bike/k0l1700185";

function fixture(name: string): string {
  return readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8");
}

describe("kijiji extractor", () => {
  it("is used by the default registry for both hosts", () => {
    for (const host of ["www.kijiji.ca", "kijiji.ca"]) {
      const result = extractPage(fixture("kijiji-search.html"), `https://${host}/b-x/k0`);
      assert.equal(result.kind, "listings");
    }
  });

  it("extracts listings, converting cents and handling price types", () => {
    const result = extractKijiji(fixture("kijiji-search.html"), PAGE_URL);
    assert.ok(result !== null && result.kind === "listings");
    assert.deepEqual(result.listings, [
      {
        id: "1001",
        url: "https://www.kijiji.ca/v-mountain-bike/ottawa/trek-hardtail/1001",
        title: "Trek hardtail mountain bike",
        price: 149,
        currency: "CAD",
        snippet: "Great shape, disc brakes. Size M.",
        location: "Ottawa",
        postedAt: "2026-10-01T12:00:00.000Z",
      },
      {
        id: "1002",
        url: "https://www.kijiji.ca/v-mountain-bike/ottawa/old-bike/1002",
        title: "Old bike, make an offer",
        price: undefined,
        currency: "CAD",
        snippet: "Contact me",
        location: "Nepean",
        postedAt: "2026-10-03T12:00:00.000Z",
      },
      {
        id: "1003",
        url: "https://www.kijiji.ca/v-mountain-bike/ottawa/free-kids-bike/1003",
        title: "Free kids bike",
        price: 0,
        currency: "CAD",
        snippet: "Pick up only",
        location: undefined,
        postedAt: undefined,
      },
      {
        id: "1004",
        url: "https://www.kijiji.ca/v-mountain-bike/ottawa/promoted-bike/1004",
        title: "Promoted only bike",
        price: undefined,
        currency: "CAD",
        snippet: "x",
        location: undefined,
        postedAt: undefined,
      },
    ]);
  });

  it("prefers the organic copy over a promoted duplicate of the same id", () => {
    const result = extractKijiji(fixture("kijiji-search.html"), PAGE_URL);
    assert.ok(result !== null && result.kind === "listings");
    const first = result.listings.find((listing) => listing.id === "1001");
    assert.equal(first?.snippet, "Great shape, disc brakes. Size M.");
    assert.equal(result.listings.filter((listing) => listing.id === "1001").length, 1);
  });

  it("keeps a promoted copy when it is the only one", () => {
    const html = fixture("kijiji-search.html").replace('"StandardListing:1001"', '"Ignored:x"');
    const result = extractKijiji(html, PAGE_URL);
    assert.ok(result !== null && result.kind === "listings");
    assert.equal(result.listings.find((listing) => listing.id === "1001")?.snippet, "Promoted copy");
  });

  it("truncates snippets to 300 characters", () => {
    const state = { props: { pageProps: { __APOLLO_STATE__: { "StandardListing:1": { id: "1", title: "t", url: "/v/1", description: "a".repeat(500), adSource: "ORGANIC" } } } } };
    const html = `<script id="__NEXT_DATA__" type="application/json">${JSON.stringify(state)}</script>`;
    const result = extractKijiji(html, PAGE_URL);
    assert.ok(result !== null && result.kind === "listings");
    assert.equal(result.listings[0]?.snippet?.length, 300);
  });

  it("returns null for malformed or missing __NEXT_DATA__ so the generic path runs", () => {
    assert.equal(extractKijiji(fixture("kijiji-malformed.html"), PAGE_URL), null);
    assert.equal(extractKijiji(fixture("kijiji-no-state.html"), PAGE_URL), null);
    assert.equal(extractKijiji("<html></html>", PAGE_URL), null);
    assert.deepEqual(extractPage(fixture("kijiji-malformed.html"), PAGE_URL), {
      kind: "unextractable",
      reason: "no structured price data",
    });
  });

  it("returns empty listings when the state exists but holds none", () => {
    const html = '<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"__APOLLO_STATE__":{}}}}</script>';
    assert.deepEqual(extractKijiji(html, PAGE_URL), { kind: "listings", listings: [] });
  });
});

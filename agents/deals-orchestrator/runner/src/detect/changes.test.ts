import assert from "node:assert/strict";
import { describe, it } from "node:test";
import type { FetchResult } from "../fetch/fetcher.ts";
import type { ExtractResult, Listing } from "../extract/types.ts";
import { MAX_SEEN_LISTING_IDS, type SourceState } from "../state/state-store.ts";
import { canSkipExtraction, observeSource } from "./changes.ts";

const NOW = new Date("2026-10-05T12:00:00.000Z");
const NOW_ISO = NOW.toISOString();

function okFetch(hash: string, extra: { etag?: string } = {}): FetchResult {
  return { outcome: "ok", body: "<html>", finalUrl: "https://a.example", httpStatus: 200, hash, ...extra };
}

function product(price: number, currency = "CAD"): ExtractResult {
  return { kind: "product", price, currency, confidence: "high", method: "json-ld", identifiers: {} };
}

function listing(id: string): Listing {
  return { id, url: id, title: id };
}

function state(overrides: Partial<SourceState> = {}): SourceState {
  return { seenListingIds: [], consecutiveFailures: 0, ...overrides };
}

describe("observeSource", () => {
  it("reports 304 as unchanged and resets failures", () => {
    const { observation, nextState } = observeSource(state({ consecutiveFailures: 2, hash: "h" }), { outcome: "not-modified" }, undefined, NOW);
    assert.deepEqual(observation, { outcome: "unchanged", reason: "not-modified" });
    assert.equal(nextState.consecutiveFailures, 0);
    assert.equal(nextState.lastCheckedAt, NOW_ISO);
    assert.equal(nextState.hash, "h");
  });

  it("reports an identical hash as unchanged without needing extraction", () => {
    const { observation } = observeSource(state({ hash: "h", lastPrice: 10, currency: "CAD" }), okFetch("h"), undefined, NOW);
    assert.deepEqual(observation, { outcome: "unchanged", reason: "same-hash" });
  });

  it("treats the first product observation as a price change with no previous price", () => {
    const { observation, nextState } = observeSource(undefined, okFetch("h1", { etag: '"e"' }), product(100), NOW);
    assert.deepEqual(observation, {
      outcome: "price-changed",
      price: 100,
      currency: "CAD",
      previousPrice: undefined,
      previousCurrency: undefined,
    });
    assert.deepEqual(
      [nextState.lastPrice, nextState.currency, nextState.hash, nextState.etag, nextState.lastChangedAt],
      [100, "CAD", "h1", '"e"', NOW_ISO],
    );
  });

  it("reports a changed price with the previous one", () => {
    const { observation } = observeSource(state({ hash: "h1", lastPrice: 120, currency: "CAD" }), okFetch("h2"), product(99), NOW);
    assert.deepEqual(observation, {
      outcome: "price-changed",
      price: 99,
      currency: "CAD",
      previousPrice: 120,
      previousCurrency: "CAD",
    });
  });

  it("reports a currency change as a price change even at the same amount", () => {
    const { observation } = observeSource(state({ hash: "h1", lastPrice: 100, currency: "CAD" }), okFetch("h2"), product(100, "USD"), NOW);
    assert.equal(observation.outcome, "price-changed");
  });

  it("reports the same price on a new hash as unchanged and records the new hash", () => {
    const { observation, nextState } = observeSource(
      state({ hash: "h1", lastPrice: 100, currency: "CAD", lastChangedAt: "earlier" }),
      okFetch("h2"),
      product(100),
      NOW,
    );
    assert.deepEqual(observation, { outcome: "unchanged", reason: "same-price" });
    assert.equal(nextState.hash, "h2");
    assert.equal(nextState.lastChangedAt, "earlier");
  });

  it("reports every listing as new on the first observation and marks them seen", () => {
    const extracted: ExtractResult = { kind: "listings", listings: [listing("a"), listing("b")] };
    const { observation, nextState } = observeSource(undefined, okFetch("h1"), extracted, NOW);
    assert.deepEqual(observation, { outcome: "new-listings", listings: [listing("a"), listing("b")] });
    assert.deepEqual(nextState.seenListingIds, ["a", "b"]);
  });

  it("reports only unseen listings afterwards", () => {
    const extracted: ExtractResult = { kind: "listings", listings: [listing("a"), listing("c")] };
    const { observation, nextState } = observeSource(state({ hash: "h1", seenListingIds: ["a", "b"] }), okFetch("h2"), extracted, NOW);
    assert.deepEqual(observation, { outcome: "new-listings", listings: [listing("c")] });
    assert.deepEqual(nextState.seenListingIds, ["a", "b", "c"]);
  });

  it("reports unchanged when no listing is new", () => {
    const extracted: ExtractResult = { kind: "listings", listings: [listing("a")] };
    const { observation } = observeSource(state({ hash: "h1", seenListingIds: ["a"] }), okFetch("h2"), extracted, NOW);
    assert.deepEqual(observation, { outcome: "unchanged", reason: "no-new-listings" });
  });

  it("keeps the seen list within the cap", () => {
    const seen = Array.from({ length: MAX_SEEN_LISTING_IDS }, (_, index) => `old${index}`);
    const extracted: ExtractResult = { kind: "listings", listings: [listing("new")] };
    const { nextState } = observeSource(state({ hash: "h1", seenListingIds: seen }), okFetch("h2"), extracted, NOW);
    assert.equal(nextState.seenListingIds.length, MAX_SEEN_LISTING_IDS);
    assert.equal(nextState.seenListingIds.at(-1), "new");
    assert.equal(nextState.seenListingIds[0], "old1");
  });

  it("reports unextractable without storing validators so the next run retries", () => {
    const extracted: ExtractResult = { kind: "unextractable", reason: "ambiguous" };
    const { observation, nextState } = observeSource(state({ hash: "old", etag: '"old"' }), okFetch("new", { etag: '"new"' }), extracted, NOW);
    assert.deepEqual(observation, { outcome: "unextractable", reason: "ambiguous" });
    assert.equal(nextState.hash, "old");
    assert.equal(nextState.etag, '"old"');
    assert.equal(nextState.consecutiveFailures, 0);
  });

  it("reports unextractable when a changed page was not extracted", () => {
    const { observation } = observeSource(state({ hash: "old" }), okFetch("new"), undefined, NOW);
    assert.equal(observation.outcome, "unextractable");
  });

  it("increments failures on fetch errors and resets them on success", () => {
    const error: FetchResult = { outcome: "error", kind: "timeout", message: "t" };
    const first = observeSource(state({ hash: "h", lastPrice: 5, currency: "CAD" }), error, undefined, NOW);
    assert.deepEqual(first.observation, { outcome: "fetch-failed", kind: "timeout" });
    assert.equal(first.nextState.consecutiveFailures, 1);
    assert.equal(first.nextState.hash, "h");
    const second = observeSource(first.nextState, error, undefined, NOW);
    assert.equal(second.nextState.consecutiveFailures, 2);
    const recovered = observeSource(second.nextState, okFetch("h2"), product(5), NOW);
    assert.equal(recovered.nextState.consecutiveFailures, 0);
  });
});

describe("blocked sources", () => {
  const blocked: FetchResult = { outcome: "error", kind: "blocked", httpStatus: 403, message: "HTTP 403" };

  it("reports blocked distinctly from fetch-failed and records blockedSince", () => {
    const first = observeSource(state({ hash: "h" }), blocked, undefined, NOW);
    assert.deepEqual(first.observation, { outcome: "blocked", since: NOW_ISO, httpStatus: 403 });
    assert.equal(first.nextState.consecutiveFailures, 1);
    assert.equal(first.nextState.blockedSince, NOW_ISO);
  });

  it("keeps the original blockedSince while still blocked", () => {
    const later = new Date("2026-10-06T12:00:00.000Z");
    const first = observeSource(undefined, blocked, undefined, NOW);
    const second = observeSource(first.nextState, blocked, undefined, later);
    assert.deepEqual(second.observation, { outcome: "blocked", since: NOW_ISO, httpStatus: 403 });
    assert.equal(second.nextState.consecutiveFailures, 2);
  });

  it("clears blockedSince and failures after a successful fetch", () => {
    const first = observeSource(undefined, blocked, undefined, NOW);
    const recovered = observeSource(first.nextState, okFetch("h"), product(5), NOW);
    assert.equal(recovered.nextState.blockedSince, undefined);
    assert.equal(recovered.nextState.consecutiveFailures, 0);
  });

  it("still reports ordinary failures as fetch-failed", () => {
    const { observation } = observeSource(undefined, { outcome: "error", kind: "http", httpStatus: 500, message: "x" }, undefined, NOW);
    assert.deepEqual(observation, { outcome: "fetch-failed", kind: "http" });
  });
});

describe("canSkipExtraction", () => {
  it("skips for 304 and identical hashes only", () => {
    assert.equal(canSkipExtraction(state({ hash: "h" }), { outcome: "not-modified" }), true);
    assert.equal(canSkipExtraction(state({ hash: "h" }), okFetch("h")), true);
    assert.equal(canSkipExtraction(state({ hash: "h" }), okFetch("other")), false);
    assert.equal(canSkipExtraction(undefined, okFetch("h")), false);
    assert.equal(canSkipExtraction(state({ hash: "h" }), { outcome: "error", kind: "network", message: "x" }), false);
  });
});

import type { FetchErrorKind, FetchResult } from "../fetch/fetcher.ts";
import type { ExtractResult, Listing } from "../extract/types.ts";
import { capSeenListingIds, emptySourceState, type SourceState } from "../state/state-store.ts";

export type UnchangedReason = "not-modified" | "same-hash" | "same-price" | "no-new-listings";

export type Observation =
  | { outcome: "unchanged"; reason: UnchangedReason }
  | {
      outcome: "price-changed";
      price: number;
      currency: string;
      previousPrice?: number | undefined;
      previousCurrency?: string | undefined;
    }
  | { outcome: "new-listings"; listings: Listing[] }
  | { outcome: "unextractable"; reason: string }
  | { outcome: "fetch-failed"; kind: Exclude<FetchErrorKind, "blocked"> }
  | { outcome: "blocked"; since: string; httpStatus?: number | undefined };

export interface Observed {
  observation: Observation;
  nextState: SourceState;
}

export function canSkipExtraction(previous: SourceState | undefined, fetched: FetchResult): boolean {
  return fetched.outcome === "not-modified" || (fetched.outcome === "ok" && previous?.hash === fetched.hash);
}

function failed(
  base: SourceState,
  fetched: Extract<FetchResult, { outcome: "error" }>,
  checkedAt: string,
): Observed {
  const failures = base.consecutiveFailures + 1;
  if (fetched.kind === "blocked") {
    const since = base.blockedSince ?? checkedAt;
    return {
      observation: { outcome: "blocked", since, httpStatus: fetched.httpStatus },
      nextState: { ...base, lastCheckedAt: checkedAt, consecutiveFailures: failures, blockedSince: since },
    };
  }
  return {
    observation: { outcome: "fetch-failed", kind: fetched.kind },
    nextState: { ...base, lastCheckedAt: checkedAt, consecutiveFailures: failures },
  };
}

export function observeSource(
  previous: SourceState | undefined,
  fetched: FetchResult,
  extracted: ExtractResult | undefined,
  now: Date,
): Observed {
  const base = previous ?? emptySourceState();
  const checkedAt = now.toISOString();
  const checked: SourceState = {
    ...base,
    lastCheckedAt: checkedAt,
    consecutiveFailures: 0,
    blockedSince: undefined,
  };

  if (fetched.outcome === "error") return failed(base, fetched, checkedAt);
  if (fetched.outcome === "not-modified") {
    return { observation: { outcome: "unchanged", reason: "not-modified" }, nextState: checked };
  }
  if (previous?.hash === fetched.hash) {
    return { observation: { outcome: "unchanged", reason: "same-hash" }, nextState: checked };
  }
  if (extracted === undefined || extracted.kind === "unextractable") {
    return {
      observation: {
        outcome: "unextractable",
        reason: extracted === undefined ? "extraction not provided" : extracted.reason,
      },
      nextState: checked,
    };
  }

  const validated: SourceState = {
    ...checked,
    etag: fetched.etag,
    lastModified: fetched.lastModified,
    hash: fetched.hash,
  };
  return extracted.kind === "product"
    ? observeProduct(base, validated, extracted.price, extracted.currency, checkedAt)
    : observeListings(base, validated, extracted.listings, checkedAt);
}

function observeProduct(
  base: SourceState,
  validated: SourceState,
  price: number,
  currency: string,
  checkedAt: string,
): Observed {
  if (base.lastPrice === price && base.currency === currency) {
    return { observation: { outcome: "unchanged", reason: "same-price" }, nextState: validated };
  }
  return {
    observation: {
      outcome: "price-changed",
      price,
      currency,
      previousPrice: base.lastPrice,
      previousCurrency: base.currency,
    },
    nextState: { ...validated, lastPrice: price, currency, lastChangedAt: checkedAt },
  };
}

function observeListings(base: SourceState, validated: SourceState, listings: Listing[], checkedAt: string): Observed {
  const seen = new Set(base.seenListingIds);
  const fresh = listings.filter((listing) => !seen.has(listing.id));
  if (fresh.length === 0) {
    return { observation: { outcome: "unchanged", reason: "no-new-listings" }, nextState: validated };
  }
  return {
    observation: { outcome: "new-listings", listings: fresh },
    nextState: {
      ...validated,
      seenListingIds: capSeenListingIds([...base.seenListingIds, ...fresh.map((listing) => listing.id)]),
      lastChangedAt: checkedAt,
    },
  };
}

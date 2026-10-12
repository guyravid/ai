import { capSeenListingIds, type SourceState } from "../state/state-store.ts";

/**
 * Listings that were not evaluated must stay unseen, and the page validators
 * must not advance, so the next run fetches and re-observes the source.
 */
export function finalizeListingsState(options: {
  previous: SourceState | undefined;
  observedNext: SourceState;
  freshIds: string[];
  evaluatedIds: string[];
}): SourceState {
  const { previous, observedNext, freshIds, evaluatedIds } = options;
  const evaluated = new Set(evaluatedIds);
  const allEvaluated = freshIds.every((id) => evaluated.has(id));
  if (allEvaluated) return observedNext;

  const previousSeen = previous?.seenListingIds ?? [];
  const seenNow = new Set(previousSeen);
  const newlySeen = freshIds.filter((id) => evaluated.has(id) && !seenNow.has(id));
  return {
    ...observedNext,
    etag: previous?.etag,
    lastModified: previous?.lastModified,
    hash: previous?.hash,
    seenListingIds: capSeenListingIds([...previousSeen, ...newlySeen]),
  };
}

export function revertToPrevious(previous: SourceState | undefined, observedNext: SourceState): SourceState {
  return {
    ...observedNext,
    etag: previous?.etag,
    lastModified: previous?.lastModified,
    hash: previous?.hash,
    lastPrice: previous?.lastPrice,
    currency: previous?.currency,
    seenListingIds: previous?.seenListingIds ?? [],
  };
}

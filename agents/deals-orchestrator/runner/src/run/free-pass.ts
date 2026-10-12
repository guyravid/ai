import { canSkipExtraction, observeSource } from "../detect/changes.ts";
import { extractPage } from "../extract/extract.ts";
import type { ExtractResult, Listing } from "../extract/types.ts";
import type { FetchResult } from "../fetch/fetcher.ts";
import type { SourceState } from "../state/state-store.ts";
import { productAlertKey } from "./alerts.ts";
import type { ItemRun, SourceEntry } from "./item-run.ts";
import { revertToPrevious } from "./seen.ts";

export interface QueuedListings {
  entry: SourceEntry;
  previous: SourceState | undefined;
  observedNext: SourceState;
  fresh: Listing[];
}

export function isSearchOnly(state: SourceState | undefined): boolean {
  return state?.blockedSince !== undefined || state?.needsLlmSince !== undefined;
}

export async function freePass(run: ItemRun, entries: SourceEntry[]): Promise<QueuedListings[]> {
  const queue: QueuedListings[] = [];
  for (const entry of entries) {
    const previous = run.sourceState(entry.url);
    if (isSearchOnly(previous)) {
      run.decide(`source ${entry.name}: skipped (search-only)`);
      continue;
    }
    const fetched = await run.deps.fetchPage(entry.url, previous ?? {});
    await observeAndApply(run, entry, previous, fetched, queue);
  }
  return queue;
}

export async function observeAndApply(
  run: ItemRun,
  entry: SourceEntry,
  previous: SourceState | undefined,
  fetched: FetchResult,
  queue: QueuedListings[] | null,
): Promise<void> {
  const extracted =
    fetched.outcome === "ok" && !canSkipExtraction(previous, fetched) ? extractPage(fetched.body, fetched.finalUrl) : undefined;
  const { observation, nextState } = observeSource(previous, fetched, extracted, run.now);
  const label = `source ${entry.name}:`;

  switch (observation.outcome) {
    case "unchanged":
      run.setSource(entry.url, nextState);
      run.decide(`${label} unchanged (${observation.reason})`);
      return;
    case "price-changed":
      await applyPrice(run, entry, previous, nextState, observation, extracted);
      return;
    case "new-listings":
      run.decide(`${label} ${observation.listings.length} new listings`);
      queue?.push({ entry, previous, observedNext: nextState, fresh: observation.listings });
      return;
    case "unextractable":
      run.setSource(entry.url, { ...nextState, needsLlmSince: nextState.needsLlmSince ?? run.nowIso });
      run.decide(`${label} unextractable (${observation.reason}), now search-only`);
      run.note(`marked search-only: ${entry.name} (${observation.reason})`);
      return;
    case "blocked":
      run.setSource(entry.url, nextState);
      run.decide(`${label} blocked, now search-only`);
      run.note(`blocked: ${entry.name}, search-only from now on`);
      return;
    case "fetch-failed":
      run.setSource(entry.url, nextState);
      run.decide(`${label} fetch failed (${observation.kind})`);
      return;
  }
}

async function applyPrice(
  run: ItemRun,
  entry: SourceEntry,
  previous: SourceState | undefined,
  nextState: SourceState,
  observation: { price: number; currency: string; previousPrice?: number | undefined },
  extracted: ExtractResult | undefined,
): Promise<void> {
  const title = extracted?.kind === "product" && extracted.name !== undefined ? extracted.name : entry.name;
  const { price, currency, previousPrice } = observation;
  run.priceEvents += 1;
  run.decide(`source ${entry.name}: price ${previousPrice ?? "first"} -> ${price} ${currency}`);
  run.note(`${entry.name}: ${previousPrice === undefined ? "first price" : `${previousPrice} ->`} ${price} ${currency}`);
  const result = await run.dispatchAlert({
    key: productAlertKey(entry.url, price),
    title,
    url: entry.url,
    observation: { price, currency, evidence: "page" },
  });
  run.setSource(entry.url, result === "failed" ? revertToPrevious(previous, nextState) : nextState);
}

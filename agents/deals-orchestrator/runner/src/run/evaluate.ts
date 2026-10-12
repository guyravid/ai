import type { Listing } from "../extract/types.ts";
import { listingAlertKey } from "./alerts.ts";
import type { QueuedListings } from "./free-pass.ts";
import { describe, type ItemRun } from "./item-run.ts";
import { finalizeListingsState } from "./seen.ts";
import { parseSearchOutput, type SearchMatch } from "./search-output.ts";

interface FlatListing {
  listing: Listing;
  queueIndex: number;
}

function chunk<T>(items: T[], size: number): T[][] {
  const chunks: T[][] = [];
  for (let start = 0; start < items.length; start += size) chunks.push(items.slice(start, start + size));
  return chunks;
}

async function recentNotes(run: ItemRun): Promise<string[]> {
  try {
    return (await run.deps.store.listNotes(run.item.id, { limit: 3 })).map((note) => note.text);
  } catch (error) {
    run.fail(`listNotes: ${describe(error)}`);
    return [];
  }
}

export async function evaluateQueued(run: ItemRun, queue: QueuedListings[]): Promise<void> {
  const flat: FlatListing[] = queue.flatMap((queued, queueIndex) => queued.fresh.map((listing) => ({ listing, queueIndex })));
  const selected = flat.slice(0, run.config.maxListingsPerItemPerRun);
  const evaluated = queue.map(() => new Set<string>());
  const counts = queue.map(() => ({ matches: 0, uncertain: 0 }));

  if (selected.length > 0) {
    const lastNotes = await recentNotes(run);
    let stopped = false;
    for (const batch of chunk(selected, run.config.listingsPerCall)) {
      if (stopped) break;
      const outcome = await run.llm("evaluate-listings", {
        spec: run.spec,
        brief: run.item.brief,
        listings: batch.map((entry) => entry.listing),
        lastNotes,
      });
      if (outcome.kind === "skipped") {
        if (outcome.reason !== "dry-run") stopped = true;
        continue;
      }
      if (outcome.kind === "failed") continue;
      const parsed = parseSearchOutput(outcome.output);
      if (parsed === null) {
        run.decide("llm evaluate-listings: invalid output, listings stay unseen");
        continue;
      }
      await applyVerdicts(run, batch, parsed.matches, evaluated, counts);
    }
  }

  const evaluatedTotal = evaluated.reduce((sum, ids) => sum + ids.size, 0);
  if (run.deps.gate.dailyCapHit) run.capSkippedListings = Math.max(0, selected.length - evaluatedTotal);
  finalizeQueue(run, queue, evaluated, counts);
}

async function applyVerdicts(
  run: ItemRun,
  batch: FlatListing[],
  matches: SearchMatch[],
  evaluated: Set<string>[],
  counts: { matches: number; uncertain: number }[],
): Promise<void> {
  const verdictById = new Map(matches.flatMap((match) => (match.listingId === undefined ? [] : [[match.listingId, match] as const])));
  for (const { listing, queueIndex } of batch) {
    const match = verdictById.get(listing.id);
    const seen = evaluated[queueIndex];
    const tally = counts[queueIndex];
    if (match === undefined || seen === undefined || tally === undefined) continue;
    seen.add(listing.id);
    if (match.verdict === "uncertain") {
      tally.uncertain += 1;
      run.note(`uncertain: ${listing.title} ${listing.url}`);
    } else if (match.verdict === "match") {
      tally.matches += 1;
      const result = await alertForMatch(run, listing, match);
      if (result === "failed") seen.delete(listing.id);
    }
  }
}

async function alertForMatch(run: ItemRun, listing: Listing, match: SearchMatch): Promise<string> {
  const price = match.price ?? listing.price;
  const currency = match.currency ?? listing.currency;
  if (price === undefined || currency === undefined) {
    run.note(`match without price: ${listing.title} ${listing.url}`);
    return "note";
  }
  return run.dispatchAlert({
    key: listingAlertKey(listing.id),
    title: listing.title,
    url: listing.url,
    observation: { price, currency, evidence: match.evidence, asOf: match.asOf },
  });
}

function finalizeQueue(
  run: ItemRun,
  queue: QueuedListings[],
  evaluated: Set<string>[],
  counts: { matches: number; uncertain: number }[],
): void {
  queue.forEach((queued, index) => {
    const ids = [...(evaluated[index] ?? [])];
    run.setSource(
      queued.entry.url,
      finalizeListingsState({
        previous: queued.previous,
        observedNext: queued.observedNext,
        freshIds: queued.fresh.map((listing) => listing.id),
        evaluatedIds: ids,
      }),
    );
    const tally = counts[index];
    run.note(
      `${queued.entry.name}: ${queued.fresh.length} new listings, ${ids.length} evaluated, ${tally?.matches ?? 0} match, ${tally?.uncertain ?? 0} uncertain`,
    );
  });
}

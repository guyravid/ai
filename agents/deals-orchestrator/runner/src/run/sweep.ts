import { extractPage } from "../extract/extract.ts";
import { StoreError } from "../store/store-error.ts";
import { emptySourceState } from "../state/state-store.ts";
import { isSweepDue } from "./schedule.ts";
import { observeAndApply, isSearchOnly } from "./free-pass.ts";
import { describe, type ItemRun, type SourceEntry } from "./item-run.ts";
import { productAlertKey } from "./alerts.ts";
import { parseSearchOutput, type NewSourceSuggestion, type SearchMatch } from "./search-output.ts";

type Classification = "free-product" | "free-listings" | "blocked" | "needs-llm";

export function sweepIsDue(run: ItemRun): boolean {
  return isSweepDue(run.now, run.state.lastSweepAt, run.role, run.config, run.options.forceSweep);
}

function splitSources(run: ItemRun, entries: SourceEntry[]): { free: SourceEntry[]; searchOnly: SourceEntry[] } {
  const searchOnly = entries.filter((entry) => isSearchOnly(run.sourceState(entry.url)));
  return { free: entries.filter((entry) => !searchOnly.includes(entry)), searchOnly };
}

export async function runSweep(run: ItemRun, entries: SourceEntry[]): Promise<void> {
  const { free, searchOnly } = splitSources(run, entries);
  const outcome = await run.llm("sweep", {
    spec: run.spec,
    brief: run.item.brief,
    knownSources: free.map(({ name, url }) => ({ name, url })),
    blockedSources: searchOnly.map(({ name, url }) => ({ name, url })),
    maxSearches: run.config.maxSearchesPerSweep,
  });
  if (outcome.kind === "skipped") {
    if (outcome.reason === "daily-cap") run.capSweepDeferred = true;
    return;
  }
  if (outcome.kind === "failed") return;
  const parsed = parseSearchOutput(outcome.output);
  if (parsed === null) {
    run.decide("llm sweep: invalid output");
    return;
  }
  run.state.lastSweepAt = run.nowIso;
  for (const match of parsed.matches) await alertForSweepMatch(run, match);
  for (const text of parsed.notes) run.note(`searcher: ${text}`);
  for (const text of parsed.specSuggestions) run.note(`suggestion: ${text}`);
  const known = new Set(entries.map((entry) => entry.url));
  for (const suggestion of parsed.newSources.slice(0, run.config.maxNewSourcesPerSweep)) {
    if (!known.has(suggestion.url)) await addNewSource(run, suggestion);
  }
}

async function alertForSweepMatch(run: ItemRun, match: SearchMatch): Promise<void> {
  const url = match.url ?? match.listingId;
  if (match.verdict === "uncertain") {
    run.note(`uncertain: ${url ?? "unknown source"} (${match.reasons.join("; ")})`);
    return;
  }
  if (match.verdict !== "match") return;
  if (url === undefined || match.price === undefined || match.currency === undefined) {
    run.note(`match without url or price: ${url ?? "unknown source"}`);
    return;
  }
  await run.dispatchAlert({
    key: productAlertKey(url, match.price),
    title: run.item.name,
    url,
    observation: { price: match.price, currency: match.currency, evidence: match.evidence, asOf: match.asOf },
  });
}

function classify(fetched: Awaited<ReturnType<ItemRun["deps"]["fetchPage"]>>): Classification {
  if (fetched.outcome === "error" && fetched.kind === "blocked") return "blocked";
  if (fetched.outcome !== "ok") return "needs-llm";
  const extracted = extractPage(fetched.body, fetched.finalUrl);
  if (extracted.kind === "product") return "free-product";
  if (extracted.kind === "listings" && extracted.listings.length > 0) return "free-listings";
  return "needs-llm";
}

async function addNewSource(run: ItemRun, suggestion: NewSourceSuggestion): Promise<void> {
  const fetched = await run.deps.fetchPage(suggestion.url, {});
  const classification = classify(fetched);
  const added = await run.addSourceSafely({ name: suggestion.name, url: suggestion.url });
  if (added === null) return;
  run.priceEvents += 1;
  run.decide(`new source ${suggestion.name}: ${classification}`);
  run.note(`source added: ${suggestion.name} (${classification}) ${suggestion.url}`);

  const entry = { name: suggestion.name, url: suggestion.url };
  if (classification === "needs-llm") {
    run.setSource(suggestion.url, { ...emptySourceState(), needsLlmSince: run.nowIso });
  } else if (classification !== "free-listings") {
    await observeAndApply(run, entry, undefined, fetched, null);
  }
  if (suggestion.kind === "search" && run.spec.type === "general") await addToLibrary(run, entry);
}

async function addToLibrary(run: ItemRun, entry: SourceEntry): Promise<void> {
  if (run.spec.category.trim() === "") return;
  try {
    await run.deps.store.addCategorySource(run.spec.category, entry);
    run.decide(`category library: added ${entry.name}`);
  } catch (error) {
    if (error instanceof StoreError && error.code === "config") {
      run.decide("category library: not configured, skipped");
    } else {
      run.fail(`addCategorySource: ${describe(error)}`);
    }
  }
}

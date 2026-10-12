import { StoreError } from "../store/store-error.ts";
import type { Item, Source } from "../store/item-store.ts";
import type { ItemState } from "../state/state-store.ts";
import { buildRunNote, dateOf } from "./notes.ts";
import { evaluateQueued } from "./evaluate.ts";
import { freePass } from "./free-pass.ts";
import { describe, ItemRun, type SourceEntry } from "./item-run.ts";
import type { ItemResult, RunDeps, RunOptions } from "./deps.ts";
import { runSweep, sweepIsDue } from "./sweep.ts";

async function collectSources(run: ItemRun): Promise<SourceEntry[]> {
  const itemSources = await run.deps.store.listSources(run.item.id);
  run.itemSources = itemSources;
  const entries = new Map<string, SourceEntry>(itemSources.map((source) => [source.url, { url: source.url, name: source.name }]));

  if (run.spec.type === "general" && run.spec.category.trim() !== "") {
    try {
      for (const source of await run.deps.store.listCategorySources(run.spec.category)) {
        if (!entries.has(source.url)) entries.set(source.url, { url: source.url, name: source.name });
      }
    } catch (error) {
      if (error instanceof StoreError && error.code === "config") run.decide("category library: not configured, skipped");
      else run.fail(`listCategorySources: ${describe(error)}`);
    }
  }
  return [...entries.values()];
}

async function promoteBestSource(run: ItemRun): Promise<void> {
  if (run.priceEvents === 0) return;
  const currency = run.spec.targetPrice?.currency;
  const priced = run.itemSources.flatMap((source) => {
    const state = run.sourceState(source.url);
    return state?.lastPrice !== undefined && state.currency === currency ? [{ source, price: state.lastPrice }] : [];
  });
  const cheapest = priced.reduce<(typeof priced)[number] | undefined>(
    (best, candidate) => (best === undefined || candidate.price < best.price ? candidate : best),
    undefined,
  );
  if (cheapest === undefined || cheapest.source.best) return;
  const currentBest = priced.find((candidate) => candidate.source.best);
  if (currentBest !== undefined && cheapest.price >= currentBest.price) return;
  try {
    await run.deps.store.promoteSource(run.item.id, cheapest.source.id);
    run.decide(`promoted best source ${cheapest.source.name} at ${cheapest.price}`);
    run.note(`best source: ${cheapest.source.name} at ${cheapest.price} ${currency ?? ""}`.trim());
  } catch (error) {
    run.fail(`promoteSource: ${describe(error)}`);
  }
}

async function writeNote(run: ItemRun): Promise<void> {
  const note = buildRunNote({ date: dateOf(run.now), lines: run.noteLines });
  if (note === null) return;
  try {
    await run.deps.store.addNote(run.item.id, note);
  } catch (error) {
    run.fail(`addNote: ${describe(error)}`);
  }
}

export interface ProcessedItem {
  result: ItemResult;
  state: ItemState;
}

export async function processItem(deps: RunDeps, options: RunOptions, item: Item, state: ItemState): Promise<ProcessedItem> {
  if (item.spec === null) throw new Error("processItem requires a spec");
  const run = new ItemRun(deps, options, item, item.spec, structuredClone(state));
  const plannedBefore = deps.gate.plannedCalls.length;

  const entries = await collectSources(run);
  const queue = await freePass(run, entries);
  await evaluateQueued(run, queue);
  if (sweepIsDue(run)) await runSweep(run, entries);
  await promoteBestSource(run);
  await writeNote(run);
  run.state.lastRunAt = run.nowIso;

  return {
    state: run.state,
    result: {
      itemId: item.id,
      name: item.name,
      role: item.role,
      due: true,
      decisions: run.decisions,
      llmCalls: run.llmCalls,
      costUsd: run.costUsd,
      tokensIn: run.tokensIn,
      tokensOut: run.tokensOut,
      alerts: run.alerts,
      errors: run.errors,
      plannedCalls: deps.gate.plannedCalls.slice(plannedBefore),
      capSkippedListings: run.capSkippedListings,
      capSweepDeferred: run.capSweepDeferred,
    },
  };
}

export type { Source };

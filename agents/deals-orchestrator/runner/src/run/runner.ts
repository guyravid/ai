import type { Item } from "../store/item-store.ts";
import { TRACKED_ROLES } from "../roles.ts";
import type { ItemState } from "../state/state-store.ts";
import { planningSender, readOnlyLedger, readOnlyState, readOnlyStore, silentTelemetry } from "./dry-run.ts";
import type { ItemResult, NoticeResult, RunDeps, RunOptions, RunSummary } from "./deps.ts";
import { describe } from "./item-run.ts";
import { localDateKey } from "./ledger.ts";
import {
  CAP_NOTICE_KEY,
  formatCapNotice,
  formatRateLimitNotice,
  rateLimitNoticeKey,
  sendNoticeOnce,
} from "./notices.ts";
import { processItem } from "./process-item.ts";
import { isItemDue } from "./schedule.ts";
import type { TrackedRole } from "./run-config.ts";

function withDryRunGuards(deps: RunDeps): RunDeps {
  return {
    ...deps,
    store: readOnlyStore(deps.store),
    stateStore: readOnlyState(deps.stateStore),
    ledger: readOnlyLedger(deps.ledger),
    telemetry: silentTelemetry,
    sender: deps.sender === null ? null : planningSender,
  };
}

function emptyResult(item: Item, due: boolean, decision: string): ItemResult {
  return {
    itemId: item.id,
    name: item.name,
    role: item.role,
    due,
    decisions: [decision],
    llmCalls: 0,
    costUsd: 0,
    tokensIn: 0,
    tokensOut: 0,
    alerts: 0,
    errors: [],
    plannedCalls: [],
    capSkippedListings: 0,
    capSweepDeferred: false,
  };
}

function notReadyReason(item: Item): string | null {
  if (item.spec === null) return "skipped: not search-ready (no spec)";
  if (item.searchReady) return null;
  const reasons = item.problems.length > 0 ? item.problems.join(", ") : `status ${item.spec.status}`;
  return `skipped: not search-ready (${reasons})`;
}

async function handleItem(deps: RunDeps, options: RunOptions, item: Item): Promise<ItemResult> {
  const notReady = notReadyReason(item);
  if (notReady !== null) return emptyResult(item, false, notReady);

  const state: ItemState = await deps.stateStore.load(item.id);
  const forced = options.itemId !== undefined || options.forceSweep;
  if (!forced && !isItemDue(deps.now(), state.lastRunAt, item.role as TrackedRole, deps.config.run)) {
    return emptyResult(item, false, "skipped: not due");
  }
  const processed = await processItem(deps, options, item, state);
  await deps.stateStore.save(item.id, processed.state);
  return processed.result;
}

async function safeHandleItem(deps: RunDeps, options: RunOptions, item: Item): Promise<ItemResult> {
  try {
    return await handleItem(deps, options, item);
  } catch (error) {
    const result = emptyResult(item, true, "error");
    result.errors.push(describe(error));
    return result;
  }
}

async function sendNotices(deps: RunDeps, options: RunOptions, items: ItemResult[], spentToday: number): Promise<NoticeResult[]> {
  const notices: NoticeResult[] = [];
  const common = { now: deps.now(), ledger: deps.ledger, sender: deps.sender, dryRun: options.dryRun };
  if (deps.gate.dailyCapHit) {
    const text = formatCapNotice({
      spentToday,
      dailyCapUsd: deps.config.run.dailyCapUsd,
      items,
      configPath: deps.configPath,
    });
    notices.push(await sendNoticeOnce({ ...common, kind: "daily-cap", key: CAP_NOTICE_KEY, text }));
  }
  const rateLimit = deps.gate.rateLimitMessage;
  if (rateLimit !== null) {
    notices.push(
      await sendNoticeOnce({ ...common, kind: "rate-limited", key: rateLimitNoticeKey(rateLimit), text: formatRateLimitNotice(rateLimit) }),
    );
  }
  return notices;
}

function stoppedReason(deps: RunDeps): string | undefined {
  const reasons: string[] = [];
  if (deps.gate.rateLimitMessage !== null) reasons.push(`rate_limited: ${deps.gate.rateLimitMessage}`);
  if (deps.gate.dailyCapHit) reasons.push("daily cap reached");
  return reasons.length === 0 ? undefined : reasons.join("; ");
}

async function appendTelemetry(deps: RunDeps, results: ItemResult[]): Promise<void> {
  for (const result of results) {
    const { plannedCalls: _planned, capSkippedListings: _skipped, capSweepDeferred: _deferred, ...fields } = result;
    await deps.telemetry.append({ type: "item", ts: deps.now().toISOString(), runId: deps.runId, ...fields });
  }
}

export async function runDeals(rawDeps: RunDeps, options: RunOptions): Promise<RunSummary> {
  const deps = options.dryRun ? withDryRunGuards(rawDeps) : rawDeps;
  const all = await deps.store.listItems({ roles: [...TRACKED_ROLES] });
  const items = options.itemId === undefined ? all : all.filter((item) => item.id === options.itemId);

  const results: ItemResult[] = [];
  for (const item of items) results.push(await safeHandleItem(deps, options, item));

  const spentToday = await deps.ledger.spentOn(localDateKey(deps.now()));
  const notices = await sendNotices(deps, options, results, spentToday);
  const processed = results.filter((result) => result.due);
  const summary: RunSummary = {
    runId: deps.runId,
    dryRun: options.dryRun,
    itemsChecked: processed.length,
    itemsSkipped: results.length - processed.length,
    llmCalls: results.reduce((sum, result) => sum + result.llmCalls, 0),
    costUsd: results.reduce((sum, result) => sum + result.costUsd, 0),
    spentToday,
    stoppedReason: items.length === 0 && options.itemId !== undefined ? "item not found" : stoppedReason(deps),
    notices,
    items: results,
  };

  await appendTelemetry(deps, results);
  const { items: _items, ...summaryLine } = summary;
  await deps.telemetry.append({ type: "summary", ts: deps.now().toISOString(), ...summaryLine });
  return summary;
}

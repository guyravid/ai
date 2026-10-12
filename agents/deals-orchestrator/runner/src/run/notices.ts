import type { ItemResult, NoticeResult } from "./deps.ts";
import { localDateKey, type Ledger } from "./ledger.ts";
import type { AlertSender } from "./telegram.ts";

export const CAP_NOTICE_KEY = "daily-cap";

export function rateLimitNoticeKey(message: string): string {
  return `rate-limited:${message}`;
}

export function formatCapNotice(options: {
  spentToday: number;
  dailyCapUsd: number;
  items: ItemResult[];
  configPath: string;
}): string {
  const skipped = options.items.filter((item) => item.capSkippedListings > 0 || item.capSweepDeferred);
  const listings = skipped.reduce((sum, item) => sum + item.capSkippedListings, 0);
  const sweeps = skipped.filter((item) => item.capSweepDeferred).length;
  const detail = skipped.map((item) => {
    const parts = [`${item.capSkippedListings} listings`];
    if (item.capSweepDeferred) parts.push("sweep deferred");
    return `${item.name} (${parts.join(", ")})`;
  });
  return [
    "Deals runner: daily LLM spend cap reached.",
    `Spent today: $${options.spentToday.toFixed(2)} of $${options.dailyCapUsd.toFixed(2)}.`,
    `Skipped this run: ${listings} listings left unevaluated, ${sweeps} sweeps deferred.`,
    `Items: ${detail.length === 0 ? "none" : detail.join("; ")}`,
    "Skipped work resumes tomorrow, or as soon as the cap is raised.",
    `To change it, set run.dailyCapUsd in ${options.configPath}`,
  ].join("\n");
}

export function formatRateLimitNotice(message: string): string {
  return [
    "Deals runner: Claude rate limit reached, so LLM calls were stopped for this run.",
    message,
    "Skipped work resumes on a later run after the limit resets.",
  ].join("\n");
}

export async function sendNoticeOnce(options: {
  kind: NoticeResult["kind"];
  key: string;
  text: string;
  now: Date;
  ledger: Ledger;
  sender: AlertSender | null;
  dryRun: boolean;
}): Promise<NoticeResult> {
  const { kind, key, text, now, ledger, sender, dryRun } = options;
  const dateKey = localDateKey(now);
  if (await ledger.hasNotice(dateKey, key)) return { kind, status: "already-sent" };
  if (dryRun) return { kind, status: "would-send" };
  if (sender === null) return { kind, status: "suppressed" };
  const result = await sender.send(text);
  if (!result.ok) return { kind, status: "failed", error: result.error };
  await ledger.markNotice(dateKey, key, now.toISOString());
  return { kind, status: "sent" };
}

import type { DealsConfig } from "../config.ts";
import type { FetchResult, PreviousFetch } from "../fetch/fetcher.ts";
import type { ItemStore } from "../store/item-store.ts";
import type { StateStore } from "../state/state-store.ts";
import type { LlmGate, PlannedCall } from "./llm-gate.ts";
import type { Ledger } from "./ledger.ts";
import type { AlertSender } from "./telegram.ts";
import type { TelemetrySink } from "./telemetry.ts";

export interface RunOptions {
  dryRun: boolean;
  itemId?: string | undefined;
  forceSweep: boolean;
}

export interface RunDeps {
  config: DealsConfig;
  store: ItemStore;
  stateStore: StateStore;
  ledger: Ledger;
  telemetry: TelemetrySink;
  sender: AlertSender | null;
  gate: LlmGate;
  fetchPage: (url: string, previous: PreviousFetch) => Promise<FetchResult>;
  now: () => Date;
  runId: string;
  configPath: string;
}

export interface ItemResult {
  itemId: string;
  name: string;
  role: string;
  due: boolean;
  decisions: string[];
  llmCalls: number;
  costUsd: number;
  tokensIn: number;
  tokensOut: number;
  alerts: number;
  errors: string[];
  plannedCalls: PlannedCall[];
  capSkippedListings: number;
  capSweepDeferred: boolean;
}

export interface NoticeResult {
  kind: "daily-cap" | "rate-limited";
  status: "sent" | "already-sent" | "failed" | "would-send" | "suppressed";
  error?: string | undefined;
}

export interface RunSummary {
  runId: string;
  dryRun: boolean;
  itemsChecked: number;
  itemsSkipped: number;
  llmCalls: number;
  costUsd: number;
  spentToday: number;
  stoppedReason?: string | undefined;
  notices: NoticeResult[];
  items: ItemResult[];
}

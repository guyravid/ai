import { StoreError } from "../store/store-error.ts";
import type { Item, Source } from "../store/item-store.ts";
import type { Spec } from "../spec.ts";
import type { ItemState, SourceState } from "../state/state-store.ts";
import { capSeenListingIds } from "../state/state-store.ts";
import {
  decideAlert,
  formatAlertMessage,
  nextBestPrice,
  type PriceObservation,
} from "./alerts.ts";
import type { RunDeps, RunOptions } from "./deps.ts";
import { outcomeTokens, type LlmOutcome } from "./llm-gate.ts";
import type { RunConfig, TrackedRole } from "./run-config.ts";
import type { SearcherMode } from "../llm/schemas.ts";

export type DispatchResult = "sent" | "failed" | "note" | "ignored" | "suppressed";

export interface SourceEntry {
  url: string;
  name: string;
}

export class ItemRun {
  readonly deps: RunDeps;
  readonly options: RunOptions;
  readonly item: Item;
  readonly spec: Spec;
  readonly role: TrackedRole;
  readonly now: Date;
  readonly state: ItemState;
  readonly decisions: string[] = [];
  readonly errors: string[] = [];
  readonly noteLines: string[] = [];
  itemSources: Source[] = [];
  llmCalls = 0;
  costUsd = 0;
  tokensIn = 0;
  tokensOut = 0;
  alerts = 0;
  capSkippedListings = 0;
  capSweepDeferred = false;
  priceEvents = 0;

  constructor(deps: RunDeps, options: RunOptions, item: Item, spec: Spec, state: ItemState) {
    this.deps = deps;
    this.options = options;
    this.item = item;
    this.spec = spec;
    this.role = item.role as TrackedRole;
    this.now = deps.now();
    this.state = state;
  }

  get config(): RunConfig {
    return this.deps.config.run;
  }

  get nowIso(): string {
    return this.now.toISOString();
  }

  decide(text: string): void {
    this.decisions.push(text);
  }

  fail(text: string): void {
    this.errors.push(text);
  }

  note(line: string): void {
    this.noteLines.push(line);
  }

  sourceState(url: string): SourceState | undefined {
    return this.state.sources[url];
  }

  setSource(url: string, state: SourceState): void {
    this.state.sources[url] = state;
  }

  async llm(mode: SearcherMode, input: unknown): Promise<LlmOutcome> {
    const outcome = await this.deps.gate.call(mode, input, this.now);
    if (outcome.kind === "skipped") {
      this.decide(`llm ${mode}: skipped (${outcome.reason})`);
      return outcome;
    }
    const usage = outcomeTokens(outcome);
    this.llmCalls += 1;
    this.costUsd += usage.costUsd;
    this.tokensIn += usage.tokensIn;
    this.tokensOut += usage.tokensOut;
    this.decide(outcome.kind === "ok" ? `llm ${mode}: ok` : `llm ${mode}: ${outcome.failure.kind}`);
    if (outcome.kind === "failed") this.fail(`llm ${mode} ${outcome.failure.kind}: ${outcome.failure.message}`);
    return outcome;
  }

  async dispatchAlert(args: {
    key: string;
    title: string;
    url: string;
    observation: PriceObservation;
  }): Promise<DispatchResult> {
    const { key, title, url, observation } = args;
    const decision = decideAlert({
      role: this.role,
      spec: this.spec,
      observation,
      bestPrice: this.state.bestPrice,
      alreadyAlerted: this.state.alertedIds.includes(key),
      config: this.config,
    });
    this.state.bestPrice = nextBestPrice(this.state.bestPrice, observation, this.spec);

    if (decision.kind === "ignore") return "ignored";
    if (decision.kind === "note-only") {
      this.note(`${title}: ${observation.price} ${observation.currency} (${decision.reason}) ${url}`);
      return "note";
    }
    return this.sendAlert(key, title, url, observation, decision.label);
  }

  private async sendAlert(
    key: string,
    title: string,
    url: string,
    observation: PriceObservation,
    label: "target" | "best-so-far",
  ): Promise<DispatchResult> {
    const { sender } = this.deps;
    if (sender === null) {
      this.decide(`alert ${label}: suppressed (telegram disabled) ${url}`);
      return "suppressed";
    }
    const targetPrice = this.spec.targetPrice;
    if (targetPrice === null) return "ignored";
    const text = formatAlertMessage({
      itemName: this.item.name,
      title,
      url,
      cardUrl: this.item.url,
      observation,
      label,
      targetPrice,
    });
    const result = await sender.send(text);
    if (!result.ok) {
      this.fail(`telegram send failed: ${result.error}`);
      return "failed";
    }
    this.state.alertedIds = capSeenListingIds([...this.state.alertedIds, key]);
    this.alerts += 1;
    this.decide(`alert ${label}: sent ${url}`);
    this.note(`alert sent (${label}${observation.evidence === "search" ? ", via search" : ""}): ${title} ${observation.price} ${observation.currency} ${url}`);
    return "sent";
  }

  async addSourceSafely(source: { name: string; url: string }): Promise<Source | null> {
    try {
      const added = await this.deps.store.addSource(this.item.id, source);
      if (!this.itemSources.some((existing) => existing.id === added.id)) this.itemSources.push(added);
      return added;
    } catch (error) {
      this.fail(`addSource ${source.url}: ${describe(error)}`);
      return null;
    }
  }
}

export function describe(error: unknown): string {
  if (error instanceof StoreError) return `${error.code}: ${error.message}`;
  return error instanceof Error ? error.message : "unknown error";
}

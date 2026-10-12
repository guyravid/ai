import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { ValidationError } from "../json.ts";
import { formatAlertMessage, nextBestPrice, productAlertKey } from "./alerts.ts";
import { NOTE_MAX_CHARS, buildRunNote } from "./notes.ts";
import { defaultRunConfig, parseRunConfig } from "./run-config.ts";
import { isItemDue, isSweepDue } from "./schedule.ts";
import { finalizeListingsState, revertToPrevious } from "./seen.ts";
import { generalSpec, isoAgo, NOW } from "./test-support.ts";
import { parseConfig } from "../config.ts";

const CONFIG = defaultRunConfig();

describe("schedule", () => {
  it("is due with no history and by cadence minus tolerance", () => {
    assert.equal(isItemDue(NOW, undefined, "to-buy", CONFIG), true);
    assert.equal(isItemDue(NOW, isoAgo(24), "to-buy", CONFIG), true);
    assert.equal(isItemDue(NOW, isoAgo(23.6), "to-buy", CONFIG), true);
    assert.equal(isItemDue(NOW, isoAgo(23), "to-buy", CONFIG), false);
    assert.equal(isItemDue(NOW, isoAgo(7.6), "escalate", CONFIG), true);
    assert.equal(isItemDue(NOW, isoAgo(60), "upgrades", CONFIG), false);
    assert.equal(isItemDue(NOW, "garbage", "upgrades", CONFIG), true);
  });

  it("sweeps by sweepDays, or when forced", () => {
    assert.equal(isSweepDue(NOW, undefined, "to-buy", CONFIG, false), true);
    assert.equal(isSweepDue(NOW, isoAgo(24 * 7), "to-buy", CONFIG, false), true);
    assert.equal(isSweepDue(NOW, isoAgo(24 * 6), "to-buy", CONFIG, false), false);
    assert.equal(isSweepDue(NOW, isoAgo(24 * 3), "escalate", CONFIG, false), true);
    assert.equal(isSweepDue(NOW, isoAgo(1), "upgrades", CONFIG, true), true);
  });
});

describe("alert helpers", () => {
  it("builds product keys and tracks the best price within max", () => {
    assert.equal(productAlertKey("https://a/p", 9.5), "https://a/p@9.5");
    const spec = generalSpec();
    assert.equal(nextBestPrice(undefined, { price: 120, currency: "CAD", evidence: "page" }, spec), 120);
    assert.equal(nextBestPrice(120, { price: 130, currency: "CAD", evidence: "page" }, spec), 120);
    assert.equal(nextBestPrice(120, { price: 110, currency: "CAD", evidence: "page" }, spec), 110);
    assert.equal(nextBestPrice(120, { price: 500, currency: "CAD", evidence: "page" }, spec), 120);
    assert.equal(nextBestPrice(120, { price: 50, currency: "USD", evidence: "page" }, spec), 120);
  });

  it("formats page and search evidence messages", () => {
    const base = {
      itemName: "Bike",
      title: "Trek",
      url: "https://x/1",
      cardUrl: "https://trello/c",
      label: "target" as const,
      targetPrice: { amount: 150, currency: "CAD" },
    };
    const page = formatAlertMessage({ ...base, observation: { price: 140, currency: "CAD", evidence: "page" } });
    assert.match(page, /140 CAD \(target 150 CAD\)/);
    assert.match(page, /Card: https:\/\/trello\/c/);
    const search = formatAlertMessage({ ...base, label: "best-so-far", observation: { price: 140, currency: "CAD", evidence: "search", asOf: "2026-10-01" } });
    assert.match(search, /best price so far/);
    assert.match(search, /Seen via search, as of 2026-10-01\. Verify on site before buying\./);
  });
});

describe("buildRunNote", () => {
  it("returns null for quiet runs", () => {
    assert.equal(buildRunNote({ date: "2026-10-05", lines: [] }), null);
  });

  it("formats lines and caps the length", () => {
    assert.equal(buildRunNote({ date: "2026-10-05", lines: ["a", "b"] }), "Run 2026-10-05\n- a\n- b");
    const long = buildRunNote({ date: "2026-10-05", lines: Array.from({ length: 200 }, (_, index) => `line number ${index} with padding`) });
    assert.equal(long?.length, NOTE_MAX_CHARS);
    assert.ok(long?.endsWith("…"));
  });
});

describe("finalizeListingsState", () => {
  const previous = { seenListingIds: ["old"], consecutiveFailures: 0, hash: "h0", etag: "e0" };
  const observedNext = { seenListingIds: ["old", "a", "b"], consecutiveFailures: 0, hash: "h1", etag: "e1" };

  it("accepts the observed state when everything was evaluated", () => {
    assert.deepEqual(finalizeListingsState({ previous, observedNext, freshIds: ["a", "b"], evaluatedIds: ["a", "b"] }), observedNext);
  });

  it("keeps skipped listings unseen and does not advance validators", () => {
    const state = finalizeListingsState({ previous, observedNext, freshIds: ["a", "b"], evaluatedIds: ["a"] });
    assert.deepEqual(state.seenListingIds, ["old", "a"]);
    assert.equal(state.hash, "h0");
    assert.equal(state.etag, "e0");
  });

  it("marks nothing seen when nothing was evaluated", () => {
    const state = finalizeListingsState({ previous: undefined, observedNext, freshIds: ["a", "b"], evaluatedIds: [] });
    assert.deepEqual(state.seenListingIds, []);
    assert.equal(state.hash, undefined);
  });

  it("reverts price state for retrying an alert", () => {
    const reverted = revertToPrevious({ ...previous, lastPrice: 5, currency: "CAD" }, { ...observedNext, lastPrice: 4, currency: "CAD" });
    assert.equal(reverted.lastPrice, 5);
    assert.equal(reverted.hash, "h0");
  });
});

describe("run config", () => {
  it("applies defaults when absent", () => {
    const config = parseRunConfig(undefined);
    assert.deepEqual(config.cadenceHours, { "to-buy": 24, escalate: 8, upgrades: 72 });
    assert.equal(config.dailyCapUsd, 1);
    assert.equal(config.perCallBudgetUsd.sweep, 0.4);
    assert.equal(config.telegram.chat, null);
    assert.ok(existsSync(join(config.agentsDir, "deal-searcher.md")));
  });

  it("merges partial overrides", () => {
    const config = parseRunConfig({ cadenceHours: { escalate: 4 }, dailyCapUsd: 2, models: { sweep: "opus" }, telegram: { chat: "123" } });
    assert.equal(config.cadenceHours.escalate, 4);
    assert.equal(config.cadenceHours["to-buy"], 24);
    assert.equal(config.models.sweep, "opus");
    assert.equal(config.models["evaluate-listings"], "sonnet");
    assert.equal(config.dailyCapUsd, 2);
    assert.deepEqual(config.telegram, { enabled: true, chat: "123" });
  });

  it("rejects invalid values", () => {
    const bad: unknown[] = [
      { dailyCapUsd: 0 },
      { dailyCapUsd: "1" },
      { cadenceHours: { weekly: 1 } },
      { cadenceHours: { "to-buy": -1 } },
      { alert: { upgradesTargetRatio: 1.5 } },
      { models: { bogus: "x" } },
      { listingsPerCall: 1.5 },
      { telegram: { enabled: "yes" } },
      "text",
    ];
    for (const candidate of bad) assert.throws(() => parseRunConfig(candidate), ValidationError);
  });

  it("is applied by parseConfig, with defaults when the section is absent", () => {
    const base = {
      storage: "trello",
      trello: { board: "b", lists: { "to-buy": "a", escalate: "b", upgrades: "c", bought: "d" } },
    };
    assert.equal(parseConfig(base).run.dailyCapUsd, 1);
    assert.equal(parseConfig({ ...base, run: { dailyCapUsd: 3 } }).run.dailyCapUsd, 3);
    assert.throws(() => parseConfig({ ...base, run: { dailyCapUsd: -1 } }), ValidationError);
  });
});

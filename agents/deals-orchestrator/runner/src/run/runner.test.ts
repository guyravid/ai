import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { runDeals } from "./runner.ts";
import type { RunOptions } from "./deps.ts";
import type { RunConfig } from "./run-config.ts";
import {
  HOUR,
  NOW,
  RATE_LIMIT_PAYLOAD,
  createHarness,
  generalSpec,
  isoAgo,
  listingsHtml,
  makeItem,
  productHtml,
  specificSpec,
  todayKey,
  type Harness,
} from "./test-support.ts";

const PRODUCT_URL = "https://shop.example/kettle";
const MARKET_URL = "https://market.example/search";
const OPTIONS: RunOptions = { dryRun: false, forceSweep: false };

function seedSource(h: Harness, url: string, name = "Shop"): void {
  h.store.sources.set("item1", [...(h.store.sources.get("item1") ?? []), { id: `s-${name}`, name, url, best: false }]);
}

function baseState(h: Harness, sources: Record<string, object> = {}, extra: object = {}): void {
  h.state.seed("item1", { lastRunAt: isoAgo(100), lastSweepAt: isoAgo(1), ...extra }, sources);
}

function approveAll(h: Harness): void {
  h.claude.handler = (input) => {
    const listings = (input["listings"] as { id: string; price?: number }[] | undefined) ?? [];
    return {
      output: {
        mode: "evaluate-listings",
        matches: listings.map((listing) => ({
          listingId: listing.id,
          verdict: "match",
          price: listing.price,
          currency: "CAD",
          reasons: ["fits"],
          evidence: "page",
        })),
        newSources: [],
        notes: [],
        specSuggestions: [],
      },
    };
  };
}

function listingBatch(count: number, price: (index: number) => number): { id: string; price: number }[] {
  return Array.from({ length: count }, (_, index) => ({ id: `L${index + 1}`, price: price(index) }));
}

describe("free tier decisions", () => {
  it("makes no LLM call and writes no note when nothing changed", async () => {
    const h = createHarness();
    h.store.items = [makeItem({ spec: specificSpec() })];
    seedSource(h, PRODUCT_URL);
    baseState(h, { [PRODUCT_URL]: { hash: "h1", lastPrice: 90, currency: "CAD" } });
    h.web.pages.set(PRODUCT_URL, { outcome: "not-modified" });

    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(summary.llmCalls, 0);
    assert.equal(h.claude.calls.length, 0);
    assert.deepEqual(h.store.writes, []);
    assert.equal(h.sender.texts.length, 0);
    assert.equal(summary.items[0]?.alerts, 0);
  });

  it("alerts on a Specific target hit with zero LLM calls", async () => {
    const h = createHarness();
    h.store.items = [makeItem({ spec: specificSpec() })];
    seedSource(h, PRODUCT_URL);
    baseState(h);
    h.web.setHtml(PRODUCT_URL, productHtml(90), "h1");

    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(summary.llmCalls, 0);
    assert.equal(h.claude.calls.length, 0);
    assert.equal(h.sender.texts.length, 1);
    assert.match(h.sender.texts[0] ?? "", /90 CAD \(target 100 CAD\)/);
    assert.match(h.sender.texts[0] ?? "", /Price read from the page/);
    assert.deepEqual(h.state.get("item1").alertedIds, [`${PRODUCT_URL}@90`]);
    assert.equal(h.state.get("item1").sources[PRODUCT_URL]?.lastPrice, 90);
    assert.match(h.store.noteTexts("item1")[0] ?? "", /first price 90 CAD/);
  });

  it("calls evaluate-listings once per batch and alerts on matches within target", async () => {
    const h = createHarness({ run: { listingsPerCall: 8 } });
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    baseState(h);
    h.web.setHtml(MARKET_URL, listingsHtml(listingBatch(10, (index) => (index < 2 ? 120 : 190))), "h1");
    approveAll(h);

    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(h.claude.callsFor("evaluate-listings"), 2);
    assert.equal(summary.llmCalls, 2);
    assert.equal(h.sender.texts.length, 2);
    assert.equal(h.state.get("item1").sources[MARKET_URL]?.seenListingIds.length, 10);
    const args = h.claude.calls[0]?.args ?? [];
    assert.deepEqual(args.slice(-2), ["--tools", ""]);
    assert.ok(args.includes("--json-schema"));
    assert.equal(h.store.noteTexts("item1").length, 1);
    assert.match(h.store.noteTexts("item1")[0] ?? "", /10 new listings, 10 evaluated, 10 match/);
  });

  it("caps listings per item per run and leaves the rest unseen", async () => {
    const h = createHarness({ run: { listingsPerCall: 8, maxListingsPerItemPerRun: 8 } });
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    baseState(h);
    h.web.setHtml(MARKET_URL, listingsHtml(listingBatch(12, () => 190)), "h1");
    approveAll(h);

    await runDeals(h.deps, OPTIONS);
    const source = h.state.get("item1").sources[MARKET_URL];
    assert.equal(h.claude.callsFor("evaluate-listings"), 1);
    assert.equal(source?.seenListingIds.length, 8);
    assert.equal(source?.hash, undefined);
  });
});

describe("evaluation failures keep listings unseen", () => {
  function setup(run: Partial<RunConfig> = {}): Harness {
    const h = createHarness({ run });
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    baseState(h);
    h.web.setHtml(MARKET_URL, listingsHtml(listingBatch(12, () => 190)), "h1");
    return h;
  }

  it("daily cap reached: no call, listings stay unseen, one notice", async () => {
    const h = setup();
    h.ledger.spent.set(todayKey(), 0.95);

    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(h.claude.calls.length, 0);
    const source = h.state.get("item1").sources[MARKET_URL];
    assert.deepEqual(source?.seenListingIds, []);
    assert.equal(source?.hash, undefined);
    assert.equal(summary.items[0]?.capSkippedListings, 12);
    assert.equal(summary.items[0]?.capSweepDeferred, false);
    assert.equal(h.sender.texts.length, 1);
    assert.match(h.sender.texts[0] ?? "", /daily LLM spend cap reached/);
  });

  it("rate limit mid-run: later calls skipped, later listings unseen, no lastSweepAt update", async () => {
    const h = setup({ listingsPerCall: 5 });
    h.state.seed("item1", { lastRunAt: isoAgo(48) }, {});
    let call = 0;
    h.claude.handler = (input) => {
      call += 1;
      if (call === 2) return { raw: RATE_LIMIT_PAYLOAD };
      const listings = (input["listings"] as { id: string }[]) ?? [];
      return {
        output: {
          mode: "evaluate-listings",
          matches: listings.map((listing) => ({ listingId: listing.id, verdict: "reject", reasons: ["no"], evidence: "page" })),
          newSources: [],
          notes: [],
          specSuggestions: [],
        },
      };
    };

    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(h.claude.calls.length, 2);
    const state = h.state.get("item1");
    assert.equal(state.sources[MARKET_URL]?.seenListingIds.length, 5);
    assert.equal(state.lastSweepAt, undefined);
    assert.match(summary.stoppedReason ?? "", /rate_limited: You've hit your session limit/);
    assert.equal(h.sender.texts.length, 1);
    assert.match(h.sender.texts[0] ?? "", /resets 2:10pm/);
  });

  it("invalid output keeps listings unseen", async () => {
    const h = setup();
    h.claude.handler = () => ({ output: { foo: 1 } });
    await runDeals(h.deps, OPTIONS);
    assert.deepEqual(h.state.get("item1").sources[MARKET_URL]?.seenListingIds, []);
  });

  it("budget failure keeps listings unseen and continues", async () => {
    const h = setup();
    h.claude.handler = () => ({ raw: { type: "result", subtype: "error_max_budget_usd", is_error: true, result: "", total_cost_usd: 0.15, usage: {} } });
    const summary = await runDeals(h.deps, OPTIONS);
    assert.deepEqual(h.state.get("item1").sources[MARKET_URL]?.seenListingIds, []);
    assert.ok(summary.items[0]?.errors.some((error) => error.includes("budget")));
    assert.equal(h.claude.callsFor("evaluate-listings"), 2);
  });

  it("omitted listings in the output stay unseen", async () => {
    const h = setup();
    h.claude.handler = (input) => {
      const listings = (input["listings"] as { id: string }[]) ?? [];
      return {
        output: {
          mode: "evaluate-listings",
          matches: listings.slice(1).map((listing) => ({ listingId: listing.id, verdict: "reject", reasons: ["no"], evidence: "page" })),
          newSources: [],
          notes: [],
          specSuggestions: [],
        },
      };
    };
    await runDeals(h.deps, OPTIONS);
    const seen = h.state.get("item1").sources[MARKET_URL]?.seenListingIds ?? [];
    assert.ok(!seen.includes("https://market.example/l/L1"));
    assert.ok(seen.includes("https://market.example/l/L2"));
  });
});

describe("alert rules", () => {
  async function priceRun(options: { role?: "to-buy" | "escalate" | "upgrades"; price: number; state?: object; alerted?: string[]; prior?: number }) {
    const h = createHarness();
    h.store.items = [makeItem({ role: options.role ?? "to-buy", spec: generalSpec({ targetPrice: { amount: 100, currency: "CAD" } }) })];
    seedSource(h, PRODUCT_URL);
    baseState(h, { [PRODUCT_URL]: { hash: "old", lastPrice: options.prior ?? 300, currency: "CAD" } }, { alertedIds: options.alerted ?? [], ...(options.state ?? {}) });
    h.web.setHtml(PRODUCT_URL, productHtml(options.price), "new");
    await runDeals(h.deps, OPTIONS);
    return h;
  }

  const table: { name: string; role: "to-buy" | "escalate" | "upgrades"; price: number; state?: object; expectAlert: boolean; match?: RegExp }[] = [
    { name: "to-buy at target", role: "to-buy", price: 100, expectAlert: true },
    { name: "to-buy between target and max is note only", role: "to-buy", price: 120, expectAlert: false },
    { name: "above max is ignored", role: "to-buy", price: 250, expectAlert: false },
    { name: "escalate new best-so-far", role: "escalate", price: 140, state: { bestPrice: 150 }, expectAlert: true, match: /best price so far/ },
    { name: "escalate not below best", role: "escalate", price: 160, state: { bestPrice: 150 }, expectAlert: false },
    { name: "escalate first observation does not alert", role: "escalate", price: 150, expectAlert: false },
    { name: "upgrades above 85 percent of target", role: "upgrades", price: 90, expectAlert: false },
    { name: "upgrades at 85 percent of target", role: "upgrades", price: 85, expectAlert: true },
  ];

  for (const row of table) {
    it(row.name, async () => {
      const h = await priceRun({ role: row.role, price: row.price, ...(row.state === undefined ? {} : { state: row.state }) });
      assert.equal(h.sender.texts.length, row.expectAlert ? 1 : 0);
      if (row.match !== undefined) assert.match(h.sender.texts[0] ?? "", row.match);
    });
  }

  it("records best price for escalate and adds a note for between-target-and-max", async () => {
    const h = await priceRun({ role: "escalate", price: 140, state: { bestPrice: 150 } });
    assert.equal(h.state.get("item1").bestPrice, 140);
    const noted = await priceRun({ price: 120 });
    assert.match(noted.store.noteTexts("item1")[0] ?? "", /between target and max/);
  });

  it("does not re-alert the same key but alerts again on a further drop", async () => {
    const same = await priceRun({ price: 90, alerted: [`${PRODUCT_URL}@90`] });
    assert.equal(same.sender.texts.length, 0);
    const lower = await priceRun({ price: 80, alerted: [`${PRODUCT_URL}@90`] });
    assert.equal(lower.sender.texts.length, 1);
    assert.deepEqual(lower.state.get("item1").alertedIds, [`${PRODUCT_URL}@90`, `${PRODUCT_URL}@80`]);
  });

  it("does not re-alert an already alerted listing", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    baseState(h, {}, { alertedIds: ["https://market.example/l/L1"] });
    h.web.setHtml(MARKET_URL, listingsHtml([{ id: "L1", price: 100 }]), "h1");
    h.claude.handler = () => ({
      output: {
        mode: "evaluate-listings",
        matches: [{ listingId: "https://market.example/l/L1", verdict: "match", price: 100, currency: "CAD", reasons: [], evidence: "page" }],
        newSources: [],
        notes: [],
        specSuggestions: [],
      },
    });
    await runDeals(h.deps, OPTIONS);
    assert.equal(h.sender.texts.length, 0);
  });

  it("treats uncertain results as notes only", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    baseState(h);
    h.web.setHtml(MARKET_URL, listingsHtml([{ id: "L1", price: 100 }]), "h1");
    h.claude.handler = () => ({
      output: {
        mode: "evaluate-listings",
        matches: [{ listingId: "https://market.example/l/L1", verdict: "uncertain", reasons: ["size?"], evidence: "page" }],
        newSources: [],
        notes: [],
        specSuggestions: [],
      },
    });
    await runDeals(h.deps, OPTIONS);
    assert.equal(h.sender.texts.length, 0);
    assert.match(h.store.noteTexts("item1")[0] ?? "", /uncertain: Bike L1/);
  });

  it("labels search-evidence alerts as unconfirmed", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    h.state.seed("item1", { lastRunAt: isoAgo(48) });
    h.claude.handler = () => ({
      output: {
        mode: "sweep",
        matches: [{ url: "https://bigbox.example/p", verdict: "match", price: 99, currency: "CAD", reasons: [], evidence: "search", asOf: "2026-10-04" }],
        newSources: [],
        notes: [],
        specSuggestions: [],
      },
    });
    await runDeals(h.deps, OPTIONS);
    assert.match(h.sender.texts[0] ?? "", /Seen via search, as of 2026-10-04\. Verify on site before buying\./);
  });

  it("does not store the alert key or advance the price when telegram fails, then retries", async () => {
    const h = createHarness();
    h.store.items = [makeItem({ spec: specificSpec() })];
    seedSource(h, PRODUCT_URL);
    baseState(h, { [PRODUCT_URL]: { hash: "old", lastPrice: 300, currency: "CAD" } });
    h.web.setHtml(PRODUCT_URL, productHtml(90), "new");
    h.sender.failNext();

    const first = await runDeals(h.deps, OPTIONS);
    assert.equal(h.sender.texts.length, 0);
    assert.deepEqual(h.state.get("item1").alertedIds, []);
    assert.equal(h.state.get("item1").sources[PRODUCT_URL]?.lastPrice, 300);
    assert.ok(first.items[0]?.errors.some((error) => error.includes("telegram send failed")));

    h.state.seed("item1", { ...h.state.get("item1"), lastRunAt: isoAgo(48) }, { [PRODUCT_URL]: h.state.get("item1").sources[PRODUCT_URL] ?? {} });
    await runDeals(h.deps, OPTIONS);
    assert.equal(h.sender.texts.length, 1);
    assert.deepEqual(h.state.get("item1").alertedIds, [`${PRODUCT_URL}@90`]);
  });
});

describe("search-only sources and sweeps", () => {
  it("marks unextractable sources search-only, skips them daily, and includes them in the sweep", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    seedSource(h, "https://spa.example/p", "Spa");
    baseState(h);
    h.web.setHtml("https://spa.example/p", "<html>rendered in browser</html>", "h1");

    const first = await runDeals(h.deps, OPTIONS);
    assert.equal(first.llmCalls, 0);
    assert.equal(h.state.get("item1").sources["https://spa.example/p"]?.needsLlmSince, NOW.toISOString());
    assert.equal(h.web.requested.length, 1);

    h.state.seed("item1", { ...h.state.get("item1"), lastRunAt: isoAgo(48) }, { "https://spa.example/p": h.state.get("item1").sources["https://spa.example/p"] ?? {} });
    const second = await runDeals(h.deps, { ...OPTIONS, forceSweep: true });
    assert.equal(h.web.requested.length, 1);
    assert.equal(h.claude.callsFor("sweep"), 1);
    const input = h.claude.calls[0]?.input as { blockedSources: { url: string }[]; knownSources: unknown[]; maxSearches: number };
    assert.deepEqual(input.blockedSources.map((source) => source.url), ["https://spa.example/p"]);
    assert.deepEqual(input.knownSources, []);
    assert.equal(input.maxSearches, 5);
    assert.equal(second.llmCalls, 1);
  });

  it("sweeps when due, not when recent, and always with --force-sweep", async () => {
    const run = async (lastSweepAt: string | undefined, forceSweep: boolean): Promise<number> => {
      const h = createHarness();
      h.store.items = [makeItem()];
      h.state.seed("item1", { lastRunAt: isoAgo(48), ...(lastSweepAt === undefined ? {} : { lastSweepAt }) });
      await runDeals(h.deps, { dryRun: false, forceSweep });
      return h.claude.callsFor("sweep");
    };
    assert.equal(await run(undefined, false), 1);
    assert.equal(await run(isoAgo(24 * 8), false), 1);
    assert.equal(await run(isoAgo(24 * 2), false), 0);
    assert.equal(await run(isoAgo(1), true), 1);
  });

  it("updates lastSweepAt only when the sweep succeeded", async () => {
    const ok = createHarness();
    ok.store.items = [makeItem()];
    ok.state.seed("item1", { lastRunAt: isoAgo(48) });
    await runDeals(ok.deps, OPTIONS);
    assert.equal(ok.state.get("item1").lastSweepAt, NOW.toISOString());

    const bad = createHarness();
    bad.store.items = [makeItem()];
    bad.state.seed("item1", { lastRunAt: isoAgo(48) });
    bad.claude.handler = () => ({ output: { nope: true } });
    await runDeals(bad.deps, OPTIONS);
    assert.equal(bad.state.get("item1").lastSweepAt, undefined);
  });

  it("adds new sources after a probe, promotes a cheaper best, and adds search sources to the library", async () => {
    const h = createHarness({ run: { maxNewSourcesPerSweep: 2 } });
    h.store.items = [makeItem({ spec: generalSpec({ targetPrice: { amount: 50, currency: "CAD" } }) })];
    h.store.sources.set("item1", [{ id: "old", name: "Old", url: PRODUCT_URL, best: true }]);
    h.state.seed("item1", { lastRunAt: isoAgo(48) }, { [PRODUCT_URL]: { hash: "h", lastPrice: 100, currency: "CAD" } });
    h.web.pages.set(PRODUCT_URL, { outcome: "not-modified" });
    h.web.setHtml("https://new.example/p", productHtml(80), "n1");
    h.web.setHtml("https://wall.example/p", "<html>x</html>", "n2");
    h.claude.handler = () => ({
      output: {
        mode: "sweep",
        matches: [],
        newSources: [
          { name: "New", url: "https://new.example/p", kind: "product", note: "" },
          { name: "Wall", url: "https://wall.example/p", kind: "search", note: "" },
          { name: "Third", url: "https://third.example/p", kind: "product", note: "" },
        ],
        notes: ["found two"],
        specSuggestions: [],
      },
    });

    await runDeals(h.deps, OPTIONS);
    assert.ok(h.store.writes.includes("addSource:item1:https://new.example/p"));
    assert.ok(h.store.writes.includes("addSource:item1:https://wall.example/p"));
    assert.ok(!h.store.writes.some((write) => write.includes("third.example")));
    assert.ok(h.store.writes.includes("addCategorySource:Bikes:https://wall.example/p"));
    const promoted = h.store.writes.find((write) => write.startsWith("promoteSource"));
    assert.ok(promoted?.startsWith("promoteSource:item1:new"));
    const state = h.state.get("item1");
    assert.equal(state.sources["https://new.example/p"]?.lastPrice, 80);
    assert.ok(state.sources["https://wall.example/p"]?.needsLlmSince !== undefined);
    assert.match(h.store.noteTexts("item1")[0] ?? "", /source added: New \(free-product\)/);
  });

  it("skips the category library silently when it is not configured", async () => {
    const h = createHarness();
    h.store.libraryConfigured = false;
    h.store.items = [makeItem()];
    baseState(h);
    const summary = await runDeals(h.deps, OPTIONS);
    assert.deepEqual(summary.items[0]?.errors, []);
    assert.ok(summary.items[0]?.decisions.includes("category library: not configured, skipped"));
  });
});

describe("selection and isolation", () => {
  it("skips items that are not search-ready", async () => {
    const h = createHarness();
    h.store.items = [makeItem({ spec: generalSpec({ status: "draft" }) }), makeItem({ id: "bare", spec: null })];
    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(summary.itemsChecked, 0);
    assert.match(summary.items[0]?.decisions[0] ?? "", /not search-ready/);
    assert.match(summary.items[1]?.decisions[0] ?? "", /no spec/);
    assert.equal(h.web.requested.length, 0);
    assert.equal(h.state.saves, 0);
  });

  it("skips items that are not due unless --item or --force-sweep is given", async () => {
    const make = (): Harness => {
      const h = createHarness();
      h.store.items = [makeItem()];
      h.state.seed("item1", { lastRunAt: isoAgo(1), lastSweepAt: isoAgo(1) });
      return h;
    };
    const skipped = await runDeals(make().deps, OPTIONS);
    assert.equal(skipped.itemsChecked, 0);
    assert.equal(skipped.items[0]?.decisions[0], "skipped: not due");
    assert.equal((await runDeals(make().deps, { ...OPTIONS, itemId: "item1" })).itemsChecked, 1);
    assert.equal((await runDeals(make().deps, { ...OPTIONS, forceSweep: true })).itemsChecked, 1);
  });

  it("treats an item as due within the tolerance window", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    h.state.seed("item1", { lastRunAt: new Date(NOW.getTime() - 24 * HOUR + 20 * 60_000).toISOString(), lastSweepAt: isoAgo(1) });
    assert.equal((await runDeals(h.deps, OPTIONS)).itemsChecked, 1);
  });

  it("ignores items outside the tracked roles and reports an unknown --item", async () => {
    const h = createHarness();
    h.store.items = [makeItem({ role: "bought" })];
    const summary = await runDeals(h.deps, { ...OPTIONS, itemId: "item1" });
    assert.equal(summary.stoppedReason, "item not found");
  });

  it("isolates errors per item", async () => {
    const h = createHarness();
    h.store.items = [makeItem({ id: "a" }), makeItem({ id: "b" })];
    h.store.failListSourcesFor = "a";
    h.state.seed("b", { lastRunAt: isoAgo(48), lastSweepAt: isoAgo(1) });
    const summary = await runDeals(h.deps, OPTIONS);
    assert.ok((summary.items[0]?.errors[0] ?? "").includes("boom"));
    assert.equal(summary.items[1]?.errors.length, 0);
    assert.equal(h.state.saves, 1);
    assert.ok(h.state.states.has("b"));
  });

  it("writes telemetry for every item plus a summary", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    baseState(h);
    await runDeals(h.deps, OPTIONS);
    assert.equal(h.telemetry.lines.length, 2);
    assert.equal(h.telemetry.lines[0]?.["type"], "item");
    assert.equal(h.telemetry.lines[0]?.["runId"], "run1");
    assert.equal(h.telemetry.lines[1]?.["type"], "summary");
    assert.equal(h.telemetry.lines[1]?.["itemsChecked"], 1);
  });
});

describe("dry run", () => {
  it("plans calls but writes nothing anywhere", async () => {
    const h = createHarness({ dryRun: true });
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    h.state.seed("item1", { lastRunAt: isoAgo(48) });
    h.web.setHtml(MARKET_URL, listingsHtml(listingBatch(10, () => 100)), "h1");

    const before = structuredClone(h.state.states.get("item1"));
    const summary = await runDeals(h.deps, { dryRun: true, forceSweep: false });
    assert.deepEqual(h.store.writes, []);
    assert.equal(h.state.saves, 0);
    assert.deepEqual(h.state.states.get("item1"), before);
    assert.equal(h.ledger.records, 0);
    assert.equal(h.ledger.marks, 0);
    assert.equal(h.sender.texts.length, 0);
    assert.equal(h.telemetry.lines.length, 0);
    assert.equal(h.claude.calls.length, 0);
    assert.equal(summary.llmCalls, 0);
    const planned = summary.items[0]?.plannedCalls ?? [];
    assert.deepEqual(planned.map((call) => call.mode), ["evaluate-listings", "evaluate-listings", "sweep"]);
    assert.equal(planned[2]?.budgetUsd, 0.4);
  });

  it("does not send an alert in dry-run", async () => {
    const h = createHarness({ dryRun: true });
    h.store.items = [makeItem({ spec: specificSpec() })];
    seedSource(h, PRODUCT_URL);
    baseState(h);
    h.web.setHtml(PRODUCT_URL, productHtml(90), "h1");
    await runDeals(h.deps, { dryRun: true, forceSweep: false });
    assert.equal(h.sender.texts.length, 0);
    assert.deepEqual(h.store.writes, []);
  });
});

describe("notices", () => {
  function capHarness(): Harness {
    const h = createHarness();
    h.store.items = [makeItem()];
    seedSource(h, MARKET_URL, "Market");
    baseState(h);
    h.web.setHtml(MARKET_URL, listingsHtml(listingBatch(3, () => 190)), "h1");
    h.ledger.spent.set(todayKey(), 0.95);
    return h;
  }

  function resetItem(h: Harness): void {
    h.state.seed("item1", { lastRunAt: isoAgo(48), lastSweepAt: isoAgo(1) });
  }

  it("sends exactly one cap notice per day across runs, with the details", async () => {
    const h = capHarness();
    const first = await runDeals(h.nextRun(), OPTIONS);
    resetItem(h);
    const second = await runDeals(h.nextRun(), OPTIONS);
    assert.equal(h.sender.texts.length, 1);
    assert.equal(first.notices[0]?.status, "sent");
    assert.equal(second.notices[0]?.status, "already-sent");
    const text = h.sender.texts[0] ?? "";
    assert.match(text, /Spent today: \$0\.95 of \$1\.00/);
    assert.match(text, /3 listings left unevaluated, 0 sweeps deferred/);
    assert.match(text, /Item item1/);
    assert.match(text, /resumes tomorrow, or as soon as the cap is raised/);
    assert.match(text, /run\.dailyCapUsd in \/cfg\/config\.json/);
  });

  it("counts deferred sweeps in the cap notice", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    h.state.seed("item1", { lastRunAt: isoAgo(48) });
    h.ledger.spent.set(todayKey(), 0.9);
    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(summary.items[0]?.capSweepDeferred, true);
    assert.match(h.sender.texts[0] ?? "", /0 listings left unevaluated, 1 sweeps deferred/);
  });

  it("retries the cap notice on the next run when the send failed", async () => {
    const h = capHarness();
    h.sender.failNext();
    const first = await runDeals(h.nextRun(), OPTIONS);
    assert.equal(first.notices[0]?.status, "failed");
    assert.equal(h.ledger.marks, 0);
    resetItem(h);
    const second = await runDeals(h.nextRun(), OPTIONS);
    assert.equal(second.notices[0]?.status, "sent");
    assert.equal(h.sender.texts.length, 1);
  });

  it("sends a new cap notice on a new day", async () => {
    const h = capHarness();
    await runDeals(h.nextRun(), OPTIONS);
    const tomorrow = createHarness({ now: new Date(NOW.getTime() + 24 * HOUR) });
    assert.equal(h.sender.texts.length, 1);
    assert.equal(tomorrow.sender.texts.length, 0);
  });

  it("dedupes the rate-limit notice per reset message", async () => {
    const h = createHarness();
    h.store.items = [makeItem()];
    h.claude.handler = () => ({ raw: RATE_LIMIT_PAYLOAD });
    const arm = (): void => h.state.seed("item1", { lastRunAt: isoAgo(48) });
    arm();
    const first = await runDeals(h.nextRun(), OPTIONS);
    arm();
    const second = await runDeals(h.nextRun(), OPTIONS);
    assert.equal(first.notices[0]?.status, "sent");
    assert.equal(second.notices[0]?.status, "already-sent");
    assert.equal(h.sender.texts.length, 1);
    assert.match(h.sender.texts[0] ?? "", /rate limit/i);

    h.claude.handler = () => ({ raw: { ...RATE_LIMIT_PAYLOAD, result: "You've hit your session limit · resets 7:00pm (America/Toronto)" } });
    arm();
    const third = await runDeals(h.nextRun(), OPTIONS);
    assert.equal(third.notices[0]?.status, "sent");
    assert.equal(h.sender.texts.length, 2);
  });

  it("reports would-send and sends nothing in dry-run", async () => {
    const h = createHarness({ dryRun: true });
    h.store.items = [makeItem()];
    h.state.seed("item1", { lastRunAt: isoAgo(48) });
    h.ledger.spent.set(todayKey(), 0.95);
    const summary = await runDeals(h.deps, { dryRun: true, forceSweep: false });
    assert.equal(summary.notices[0]?.status, "would-send");
    assert.equal(h.sender.texts.length, 0);
    assert.equal(h.ledger.marks, 0);
  });

  it("suppresses notices when telegram is disabled", async () => {
    const h = createHarness({ telegram: false });
    h.store.items = [makeItem()];
    h.state.seed("item1", { lastRunAt: isoAgo(48) });
    h.ledger.spent.set(todayKey(), 0.95);
    const summary = await runDeals(h.deps, OPTIONS);
    assert.equal(summary.notices[0]?.status, "suppressed");
  });
});

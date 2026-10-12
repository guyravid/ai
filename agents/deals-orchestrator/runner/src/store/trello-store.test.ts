import assert from "node:assert/strict";
import { describe, it } from "node:test";
import type { TrelloConfig } from "../config.ts";
import { emptyGeneralSpec, type GeneralSpec } from "../spec.ts";
import { emptyLayout, serializeDescription } from "../template.ts";
import { createFakeExec, errorEnvelope, okEnvelope } from "./fake-exec.ts";
import { StoreError } from "./store-error.ts";
import { TrelloStore } from "./trello-store.ts";

const CONFIG: TrelloConfig = {
  board: "board1",
  lists: { "to-buy": "L-buy", escalate: "L-esc", upgrades: "L-up", bought: "L-bought", "source-library": null },
};
const LIBRARY_CONFIG: TrelloConfig = { ...CONFIG, lists: { ...CONFIG.lists, "source-library": "L-lib" } };

const CARD_FIELDS = "id,name,desc,idList,url";
const BIG = ["--max-string", "200000", "--max-bytes", "8000000"];

function readySpec(): GeneralSpec {
  return {
    ...emptyGeneralSpec(),
    status: "ready",
    category: "Bikes",
    use: "Trails",
    condition: "used-ok",
    targetPrice: { amount: 150, currency: "CAD" },
    maxPrice: { amount: 200, currency: "CAD" },
    pickupRadiusKm: 50,
    mustHave: ["Disc brakes"],
  };
}

function descriptionFor(spec: GeneralSpec, extra = ""): string {
  const base = serializeDescription({ spec, brief: null, notes: "my notes", layout: emptyLayout() });
  return extra === "" ? base : `${base}\n\n${extra}`;
}

function failingExec(): never {
  throw new Error("exec must not be called");
}

describe("items", () => {
  it("lists one role with the exact argv and parses cards", async () => {
    const { exec, calls } = createFakeExec(() =>
      okEnvelope([
        { id: "c1", name: "Bike", desc: descriptionFor(readySpec()), idList: "L-buy", url: "https://t/c1" },
        { id: "c2", name: "Bare", idList: "L-buy" },
      ]),
    );
    const items = await new TrelloStore(CONFIG, exec).listItems({ roles: ["to-buy"] });

    assert.deepEqual(calls, [
      ["cards", "list", "--board", "board1", "--list-id", "L-buy", "--fields", CARD_FIELDS, "--limit", "1000", ...BIG],
    ]);
    assert.equal(items.length, 2);
    assert.equal(items[0]?.searchReady, true);
    assert.equal(items[0]?.role, "to-buy");
    assert.equal(items[0]?.url, "https://t/c1");
    assert.equal(items[1]?.spec, null);
    assert.deepEqual(items[1]?.problems, ["no spec"]);
  });

  it("defaults to all item roles and skips the unconfigured source library", async () => {
    const { exec, calls } = createFakeExec(() => okEnvelope(null));
    assert.deepEqual(await new TrelloStore(CONFIG, exec).listItems(), []);
    assert.deepEqual(
      calls.map((call) => call[5]),
      ["L-buy", "L-esc", "L-up", "L-bought"],
    );
  });

  it("follows pagination cursors", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args.includes("--cursor")
        ? okEnvelope([{ id: "c2", name: "B", idList: "L-buy" }], { hasMore: false, nextCursor: "" })
        : okEnvelope([{ id: "c1", name: "A", idList: "L-buy" }], { hasMore: true, nextCursor: "CUR" }),
    );
    const items = await new TrelloStore(CONFIG, exec).listItems({ roles: ["to-buy"] });
    assert.deepEqual(items.map((item) => item.id), ["c1", "c2"]);
    assert.deepEqual(calls[1], ["cards", "list", "--board", "board1", "--list-id", "L-buy", "--cursor", "CUR"]);
  });

  it("gets an item and maps its list to a role", async () => {
    const { exec, calls } = createFakeExec(() =>
      okEnvelope({ id: "c1", name: "Bike", desc: "", idList: "L-esc", url: "u" }),
    );
    const item = await new TrelloStore(CONFIG, exec).getItem("c1");
    assert.deepEqual(calls, [["cards", "get", "c1", "--fields", CARD_FIELDS, ...BIG]]);
    assert.equal(item.role, "escalate");
  });

  it("fails on a card in an unmapped list", async () => {
    const { exec } = createFakeExec(() => okEnvelope({ id: "c1", name: "x", idList: "L-other" }));
    await assert.rejects(new TrelloStore(CONFIG, exec).getItem("c1"), { code: "config" });
  });

  it("creates an item with the serialized spec and --confirm", async () => {
    const { exec, calls } = createFakeExec(() =>
      okEnvelope({ id: "new1", name: "Bike", idList: "L-buy", url: "https://t/new1" }),
    );
    const item = await new TrelloStore(CONFIG, exec).createItem({ role: "to-buy", name: "Bike", spec: readySpec() });

    const call = calls[0] ?? [];
    assert.deepEqual(call.slice(0, 6), ["cards", "create", "--list-id", "L-buy", "--name", "Bike"]);
    assert.equal(call[6], "--desc");
    assert.match(call[7] ?? "", /^## Deal Spec\n- \*\*Type:\*\* general/);
    assert.equal(call[8], "--confirm");
    assert.equal(call.length, 9);
    assert.equal(item.id, "new1");
    assert.equal(item.searchReady, true);
  });

  it("updates the spec while preserving unknown card content", async () => {
    const original = descriptionFor(readySpec(), "## Garage\nStored at the cottage");
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "get"
        ? okEnvelope({ id: "c1", name: "Bike", desc: original, idList: "L-buy", url: "u" })
        : okEnvelope({ id: "c1" }),
    );
    const newSpec: GeneralSpec = { ...readySpec(), use: "Winter commuting" };
    const item = await new TrelloStore(CONFIG, exec).updateItem("c1", { spec: newSpec });

    const write = calls[1] ?? [];
    assert.deepEqual(write.slice(0, 4), ["cards", "update", "c1", "--desc"]);
    assert.equal(write[5], "--confirm");
    const description = write[4] ?? "";
    assert.match(description, /\*\*Use:\*\* Winter commuting/);
    assert.ok(description.includes("## Garage\nStored at the cottage"));
    assert.ok(description.includes("## Notes\nmy notes"));
    assert.equal(item.spec?.use, "Winter commuting");
  });

  it("updates brief and notes", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "get"
        ? okEnvelope({ id: "c1", name: "Bike", desc: descriptionFor(readySpec()), idList: "L-buy" })
        : okEnvelope({ id: "c1" }),
    );
    const brief = { keywords: ["mtb"], synonyms: [], exclude: [], updated: "2026-10-05", rules: [], unknownLines: [], unknownSections: [] };
    const item = await new TrelloStore(CONFIG, exec).updateItem("c1", { brief, notes: "new notes" });
    const description = calls[1]?.[4] ?? "";
    assert.match(description, /## Search brief\n- \*\*Keywords:\*\* mtb/);
    assert.match(description, /## Notes\nnew notes$/);
    assert.deepEqual(item.brief?.keywords, ["mtb"]);
  });

  it("renames first, then updates the description", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "get"
        ? okEnvelope({ id: "c1", name: "New", desc: descriptionFor(readySpec()), idList: "L-buy" })
        : okEnvelope({ id: "c1" }),
    );
    const item = await new TrelloStore(CONFIG, exec).updateItem("c1", { name: "New", notes: "n" });
    assert.deepEqual(calls[0], ["cards", "rename", "c1", "--name", "New", "--confirm"]);
    assert.equal(calls[1]?.[1], "get");
    assert.equal(calls[2]?.[1], "update");
    assert.equal(item.name, "New");
  });

  it("only renames when no description change is requested", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "get" ? okEnvelope({ id: "c1", name: "New", desc: "", idList: "L-buy" }) : okEnvelope({ id: "c1" }),
    );
    const item = await new TrelloStore(CONFIG, exec).updateItem("c1", { name: "New" });
    assert.deepEqual(calls.map((call) => call[1]), ["rename", "get"]);
    assert.equal(item.name, "New");
  });

  it("moves an item and re-reads it", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "move"
        ? okEnvelope({ id: "c1" })
        : okEnvelope({ id: "c1", name: "Bike", desc: "", idList: "L-bought", url: "u" }),
    );
    const item = await new TrelloStore(CONFIG, exec).moveItem("c1", "bought");
    assert.deepEqual(calls[0], ["cards", "move", "c1", "--list-id", "L-bought", "--confirm"]);
    assert.equal(item.role, "bought");
  });
});

describe("errors", () => {
  it("turns an ok:false envelope into a StoreError", async () => {
    const { exec } = createFakeExec(() => errorEnvelope("rate_limited", "slow down", true));
    await assert.rejects(new TrelloStore(CONFIG, exec).getItem("c1"), (error: unknown) => {
      assert.ok(error instanceof StoreError);
      assert.equal(error.code, "rate_limited");
      assert.equal(error.message, "slow down");
      assert.equal(error.retriable, true);
      return true;
    });
  });

  it("rejects output that is not a JSON envelope", async () => {
    const { exec } = createFakeExec(() => "boom");
    await assert.rejects(new TrelloStore(CONFIG, exec).getItem("c1"), { code: "internal" });
  });

  it("rejects malformed card data", async () => {
    const { exec } = createFakeExec(() => okEnvelope({ name: "no id" }));
    await assert.rejects(new TrelloStore(CONFIG, exec).getItem("c1"), { code: "internal" });
  });
});

const ATTACHMENTS = [
  { id: "a1", name: "Best Source - Kijiji", url: "https://kijiji.ca/s" },
  { id: "a2", name: "Source - Marketplace", url: "https://fb.com/m" },
  { id: "a3", name: "design.pdf", url: "https://x/design.pdf" },
  { id: "a4", name: "source - Lowercase", url: "https://l.example" },
];

function attachmentExec(attachments: unknown[] = ATTACHMENTS) {
  return createFakeExec((args) => {
    if (args[1] === "attachments") return okEnvelope(attachments);
    if (args[1] === "attach-url") return okEnvelope({ id: "new1", name: args[args.indexOf("--name") + 1], url: args[5] });
    return okEnvelope({});
  });
}

describe("sources", () => {
  it("lists only prefixed attachments and flags the best", async () => {
    const { exec, calls } = attachmentExec();
    const sources = await new TrelloStore(CONFIG, exec).listSources("c1");
    assert.deepEqual(calls, [["cards", "attachments", "c1", "--fields", "id,name,url", "--limit", "1000"]]);
    assert.deepEqual(sources, [
      { id: "a1", name: "Kijiji", url: "https://kijiji.ca/s", best: true },
      { id: "a2", name: "Marketplace", url: "https://fb.com/m", best: false },
      { id: "a4", name: "Lowercase", url: "https://l.example", best: false },
    ]);
  });

  it("adds a plain source", async () => {
    const { exec, calls } = attachmentExec();
    const source = await new TrelloStore(CONFIG, exec).addSource("c1", { name: "eBay", url: "https://ebay.ca/s" });
    assert.deepEqual(calls[1], [
      "cards", "attach-url", "c1", "--url", "https://ebay.ca/s", "--name", "Source - eBay", "--confirm",
    ]);
    assert.equal(calls.length, 2);
    assert.deepEqual(source, { id: "new1", name: "eBay", url: "https://ebay.ca/s", best: false });
  });

  it("is a no-op for a duplicate URL", async () => {
    const { exec, calls } = attachmentExec();
    const source = await new TrelloStore(CONFIG, exec).addSource("c1", {
      name: "Other name",
      url: "https://fb.com/m",
      best: true,
    });
    assert.equal(calls.length, 1);
    assert.equal(source.id, "a2");
  });

  it("demotes the current best when adding a new best", async () => {
    const { exec, calls } = attachmentExec();
    const source = await new TrelloStore(CONFIG, exec).addSource("c1", {
      name: "eBay",
      url: "https://ebay.ca/s",
      best: true,
    });
    assert.deepEqual(calls.slice(1), [
      ["cards", "attach-url", "c1", "--url", "https://kijiji.ca/s", "--name", "Source - Kijiji", "--confirm"],
      ["cards", "detach", "c1", "--attachment", "a1", "--confirm"],
      ["cards", "attach-url", "c1", "--url", "https://ebay.ca/s", "--name", "Best Source - eBay", "--confirm"],
    ]);
    assert.equal(source.best, true);
  });

  it("adds a best source without demoting when none exists", async () => {
    const { exec, calls } = attachmentExec([]);
    await new TrelloStore(CONFIG, exec).addSource("c1", { name: "eBay", url: "https://ebay.ca/s", best: true });
    assert.equal(calls.length, 2);
    assert.equal(calls[1]?.[6], "Best Source - eBay");
  });

  it("promotes a source, demoting the old best", async () => {
    const { exec, calls } = attachmentExec();
    const promoted = await new TrelloStore(CONFIG, exec).promoteSource("c1", "a2");
    assert.deepEqual(calls.slice(1), [
      ["cards", "attach-url", "c1", "--url", "https://kijiji.ca/s", "--name", "Source - Kijiji", "--confirm"],
      ["cards", "detach", "c1", "--attachment", "a1", "--confirm"],
      ["cards", "attach-url", "c1", "--url", "https://fb.com/m", "--name", "Best Source - Marketplace", "--confirm"],
      ["cards", "detach", "c1", "--attachment", "a2", "--confirm"],
    ]);
    assert.equal(promoted.best, true);
    assert.equal(promoted.name, "Marketplace");
  });

  it("detaches nothing when an attach fails", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "attachments"
        ? okEnvelope(ATTACHMENTS)
        : args[1] === "attach-url"
          ? errorEnvelope("upstream", "attach failed")
          : okEnvelope({}),
    );
    await assert.rejects(new TrelloStore(CONFIG, exec).promoteSource("c1", "a2"), { code: "upstream" });
    assert.ok(calls.every((call) => call[1] !== "detach"));
  });

  it("reports a partial failure naming the leftover attachment when detach fails", async () => {
    const { exec } = createFakeExec((args) =>
      args[1] === "attachments"
        ? okEnvelope(ATTACHMENTS)
        : args[1] === "attach-url"
          ? okEnvelope({ id: "new1", name: "x", url: "u" })
          : errorEnvelope("network", "boom"),
    );
    await assert.rejects(new TrelloStore(CONFIG, exec).promoteSource("c1", "a2"), (error: unknown) => {
      assert.ok(error instanceof StoreError);
      assert.equal(error.code, "partial");
      assert.match(error.message, /a1/);
      assert.match(error.message, /new1/);
      return true;
    });
  });

  it("de-duplicates sources by URL, preferring the best", async () => {
    const { exec } = attachmentExec([
      { id: "x1", name: "Source - Kijiji", url: "https://k" },
      { id: "x2", name: "Best Source - Kijiji", url: "https://k" },
      { id: "x3", name: "Source - Other", url: "https://o" },
      { id: "x4", name: "Source - Other again", url: "https://o" },
    ]);
    const sources = await new TrelloStore(CONFIG, exec).listSources("c1");
    assert.deepEqual(sources.map((source) => [source.id, source.best]), [["x2", true], ["x3", false]]);
  });

  it("does nothing when promoting the current best", async () => {
    const { exec, calls } = attachmentExec();
    const promoted = await new TrelloStore(CONFIG, exec).promoteSource("c1", "a1");
    assert.equal(calls.length, 1);
    assert.equal(promoted.id, "a1");
  });

  it("fails when promoting or removing an unknown or non-source attachment", async () => {
    const store = new TrelloStore(CONFIG, attachmentExec().exec);
    await assert.rejects(store.promoteSource("c1", "missing"), { code: "not_found" });
    await assert.rejects(store.promoteSource("c1", "a3"), { code: "not_found" });
    await assert.rejects(store.removeSource("c1", "a3"), { code: "not_found" });
  });

  it("removes a source with detach", async () => {
    const { exec, calls } = attachmentExec();
    await new TrelloStore(CONFIG, exec).removeSource("c1", "a2");
    assert.deepEqual(calls[1], ["cards", "detach", "c1", "--attachment", "a2", "--confirm"]);
  });
});

describe("notes", () => {
  it("adds a note with --confirm", async () => {
    const { exec, calls } = createFakeExec(() =>
      okEnvelope({ id: "n1", date: "2026-10-05T10:00:00.000Z", data: { text: "found one" } }),
    );
    const note = await new TrelloStore(CONFIG, exec).addNote("c1", "found one");
    assert.deepEqual(calls, [["cards", "comment", "c1", "--text", "found one", "--confirm"]]);
    assert.deepEqual(note, { id: "n1", date: "2026-10-05T10:00:00.000Z", text: "found one" });
  });

  it("lists notes newest first with a limit", async () => {
    const { exec, calls } = createFakeExec(() =>
      okEnvelope([
        { id: "n2", date: "2026-10-05", data: { text: "second" } },
        { id: "n1", date: "2026-10-04", data: { text: "first" } },
      ]),
    );
    const notes = await new TrelloStore(CONFIG, exec).listNotes("c1", { limit: 3 });
    assert.deepEqual(calls, [
      ["cards", "comments", "c1", "--fields", "id,date,data.text", "--limit", "3", "--max-string", "20000"],
    ]);
    assert.deepEqual(notes.map((note) => note.text), ["second", "first"]);
  });

  it("returns no notes for an empty result", async () => {
    const { exec } = createFakeExec(() => okEnvelope(null));
    assert.deepEqual(await new TrelloStore(CONFIG, exec).listNotes("c1", { limit: 3 }), []);
  });
});

describe("category library", () => {
  it("fails with a config error when the source library is not configured", async () => {
    const store = new TrelloStore(CONFIG, failingExec);
    await assert.rejects(store.listCategorySources("Bikes"), { code: "config" });
    await assert.rejects(store.addCategorySource("Bikes", { name: "n", url: "u" }), { code: "config" });
  });

  it("finds the category card case-insensitively", async () => {
    const { exec, calls } = createFakeExec((args) =>
      args[1] === "list"
        ? okEnvelope([{ id: "cat1", name: "bikes", idList: "L-lib" }])
        : okEnvelope([{ id: "a1", name: "Source - Kijiji", url: "https://k" }]),
    );
    const sources = await new TrelloStore(LIBRARY_CONFIG, exec).listCategorySources("Bikes");
    assert.deepEqual(calls[0], [
      "cards", "list", "--board", "board1", "--list-id", "L-lib", "--fields", "id,name,idList", "--limit", "1000",
    ]);
    assert.deepEqual(calls[1]?.slice(0, 3), ["cards", "attachments", "cat1"]);
    assert.equal(sources.length, 1);
  });

  it("returns no sources for an unknown category without creating it", async () => {
    const { exec, calls } = createFakeExec(() => okEnvelope([]));
    assert.deepEqual(await new TrelloStore(LIBRARY_CONFIG, exec).listCategorySources("Bikes"), []);
    assert.equal(calls.length, 1);
  });

  it("creates the category card on demand before adding a source", async () => {
    const { exec, calls } = createFakeExec((args) => {
      if (args[1] === "list") return okEnvelope([]);
      if (args[1] === "create") return okEnvelope({ id: "cat9", name: "Bikes", idList: "L-lib" });
      if (args[1] === "attachments") return okEnvelope([]);
      return okEnvelope({ id: "att1", name: "Source - Kijiji", url: "https://k" });
    });
    const source = await new TrelloStore(LIBRARY_CONFIG, exec).addCategorySource("Bikes", {
      name: "Kijiji",
      url: "https://k",
    });
    assert.deepEqual(calls[1], ["cards", "create", "--list-id", "L-lib", "--name", "Bikes", "--confirm"]);
    assert.deepEqual(calls[3], [
      "cards", "attach-url", "cat9", "--url", "https://k", "--name", "Source - Kijiji", "--confirm",
    ]);
    assert.equal(source.id, "att1");
  });
});

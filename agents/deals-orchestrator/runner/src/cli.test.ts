import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { runCli, type CliDeps } from "./cli.ts";
import type { FetchResult } from "./fetch/fetcher.ts";
import type { Item, ItemStore } from "./store/item-store.ts";
import { StoreError } from "./store/store-error.ts";

const ITEM: Item = {
  id: "c1",
  name: "Bike",
  role: "to-buy",
  url: "https://t/c1",
  spec: null,
  brief: null,
  searchReady: false,
  problems: ["no spec"],
};

type Call = { method: string; args: unknown[] };

function createDeps(
  files: Record<string, string> = {},
  stdin = "",
  fetchResult: FetchResult = { outcome: "not-modified" },
): { deps: CliDeps; calls: Call[] } {
  const calls: Call[] = [];
  const record =
    <T>(method: string, result: T) =>
    async (...args: unknown[]): Promise<T> => {
      calls.push({ method, args });
      return result;
    };
  const store: ItemStore = {
    listItems: record("listItems", [ITEM]),
    getItem: record("getItem", ITEM),
    createItem: record("createItem", ITEM),
    updateItem: record("updateItem", ITEM),
    moveItem: record("moveItem", { ...ITEM, role: "bought" as const }),
    listSources: record("listSources", []),
    addSource: record("addSource", { id: "s1", name: "Kijiji", url: "https://k", best: true }),
    promoteSource: record("promoteSource", { id: "s2", name: "Kijiji", url: "https://k", best: true }),
    removeSource: record("removeSource", undefined),
    addNote: record("addNote", { id: "n1", date: "d", text: "t" }),
    listNotes: record("listNotes", []),
    listCategorySources: record("listCategorySources", []),
    addCategorySource: record("addCategorySource", { id: "s3", name: "n", url: "u", best: false }),
  };
  const deps: CliDeps = {
    getStore: async () => store,
    readFile: async (path) => {
      const content = files[path];
      if (content === undefined) throw new Error(`no such file ${path}`);
      return content;
    },
    readStdin: async () => stdin,
    fetchUrl: async (url) => {
      calls.push({ method: "fetchUrl", args: [url] });
      return fetchResult;
    },
  };
  return { deps, calls };
}

function parseOutput(output: string): { ok: boolean; data?: unknown; error?: { code: string; message: string } } {
  return JSON.parse(output);
}

describe("cli happy paths", () => {
  it("lists items with role filters", async () => {
    const { deps, calls } = createDeps();
    const result = await runCli(["items", "list", "--role", "to-buy", "--role", "escalate"], deps);
    assert.equal(result.exitCode, 0);
    assert.deepEqual(parseOutput(result.output), { ok: true, data: [ITEM] });
    assert.deepEqual(calls[0], { method: "listItems", args: [{ roles: ["to-buy", "escalate"] }] });
  });

  it("lists items without a filter", async () => {
    const { deps, calls } = createDeps();
    await runCli(["items", "list"], deps);
    assert.deepEqual(calls[0]?.args, [undefined]);
  });

  it("creates an item from a spec file", async () => {
    const spec = JSON.stringify({ type: "general", category: "Bikes", mustHave: ["Disc brakes"] });
    const { deps, calls } = createDeps({ "spec.json": spec });
    const result = await runCli(["items", "create", "--role", "to-buy", "--name", "Bike", "--spec-file", "spec.json"], deps);
    assert.equal(result.exitCode, 0);
    const [arg] = calls[0]?.args ?? [];
    assert.deepEqual(arg, {
      role: "to-buy",
      name: "Bike",
      spec: {
        type: "general",
        status: "draft",
        category: "Bikes",
        use: "",
        condition: null,
        targetPrice: null,
        maxPrice: null,
        location: "",
        pickupRadiusKm: null,
        shippingOk: null,
        mustHave: ["Disc brakes"],
        niceToHave: [],
        dealBreakers: [],
        fit: [],
        unknownLines: [],
        unknownSections: [],
      },
    });
  });

  it("reads the spec from stdin when the file is -", async () => {
    const { deps, calls } = createDeps({}, JSON.stringify({ type: "specific", productName: "X" }));
    const result = await runCli(["items", "create", "--role", "to-buy", "--name", "X", "--spec-file", "-"], deps);
    assert.equal(result.exitCode, 0);
    assert.equal(calls[0]?.method, "createItem");
  });

  it("updates notes from a file", async () => {
    const { deps, calls } = createDeps({ "notes.md": "hello" });
    const result = await runCli(["items", "update", "c1", "--notes-file", "notes.md"], deps);
    assert.equal(result.exitCode, 0);
    assert.deepEqual(calls[0], { method: "updateItem", args: ["c1", { notes: "hello" }] });
  });

  it("validates an item", async () => {
    const { deps } = createDeps();
    const result = await runCli(["items", "validate", "c1"], deps);
    assert.deepEqual(parseOutput(result.output).data, { id: "c1", searchReady: false, problems: ["no spec"] });
  });

  it("adds a best source", async () => {
    const { deps, calls } = createDeps();
    const result = await runCli(["sources", "add", "c1", "--name", "Kijiji", "--url", "https://k", "--best"], deps);
    assert.equal(result.exitCode, 0);
    assert.deepEqual(calls[0], {
      method: "addSource",
      args: ["c1", { name: "Kijiji", url: "https://k", best: true }],
    });
  });

  it("promotes and removes sources", async () => {
    const { deps, calls } = createDeps();
    await runCli(["sources", "promote", "c1", "s2"], deps);
    const removed = await runCli(["sources", "remove", "c1", "s2"], deps);
    assert.deepEqual(calls.map((call) => call.method), ["promoteSource", "removeSource"]);
    assert.deepEqual(parseOutput(removed.output).data, { removed: "s2" });
  });

  it("adds a note from stdin and lists with a limit", async () => {
    const { deps, calls } = createDeps({}, "from stdin");
    await runCli(["notes", "add", "c1", "--text", "-"], deps);
    await runCli(["notes", "list", "c1", "--limit", "5"], deps);
    assert.deepEqual(calls[0], { method: "addNote", args: ["c1", "from stdin"] });
    assert.deepEqual(calls[1], { method: "listNotes", args: ["c1", { limit: 5 }] });
  });

  it("handles category commands", async () => {
    const { deps, calls } = createDeps();
    await runCli(["categories", "sources", "Bikes"], deps);
    await runCli(["categories", "add-source", "Bikes", "--name", "n", "--url", "u"], deps);
    assert.deepEqual(calls.map((call) => call.method), ["listCategorySources", "addCategorySource"]);
  });
});

describe("cli probe", () => {
  const html =
    '<script type="application/ld+json">{"@type":"Product","name":"Kettle","offers":{"@type":"Offer","price":"39.99","priceCurrency":"EUR"}}</script>';

  it("fetches and extracts one URL without touching the store", async () => {
    const { deps, calls } = createDeps({}, "", {
      outcome: "ok",
      body: html,
      finalUrl: "https://a.example/kettle",
      httpStatus: 200,
      hash: "h",
    });
    deps.getStore = async () => {
      throw new Error("store must not be used");
    };
    const result = await runCli(["probe", "https://a.example/kettle"], deps);
    assert.equal(result.exitCode, 0);
    const data = parseOutput(result.output).data as { fetch: Record<string, unknown>; extraction: Record<string, unknown> };
    assert.equal(data.fetch["bytes"], Buffer.byteLength(html));
    assert.equal(data.fetch["body"], undefined);
    assert.equal(data.extraction["price"], 39.99);
    assert.equal((parseOutput(result.output).data as { monitorable: string }).monitorable, "free");
    assert.deepEqual(calls, [{ method: "fetchUrl", args: ["https://a.example/kettle"] }]);
  });

  it("returns a successful result describing a fetch failure", async () => {
    const { deps } = createDeps({}, "", { outcome: "error", kind: "http", httpStatus: 403, message: "HTTP 403" });
    const result = await runCli(["probe", "https://a.example/x"], deps);
    assert.equal(result.exitCode, 0);
    const data = parseOutput(result.output).data as { fetch: { kind: string }; extraction: null };
    assert.equal(data.fetch.kind, "http");
    assert.equal(data.extraction, null);
  });

  async function monitorableFor(fetched: FetchResult): Promise<string> {
    const { deps } = createDeps({}, "", fetched);
    const result = await runCli(["probe", "https://a.example/x"], deps);
    return (parseOutput(result.output).data as { monitorable: string }).monitorable;
  }

  function okPage(body: string): FetchResult {
    return { outcome: "ok", body, finalUrl: "https://a.example/x", httpStatus: 200, hash: "h" };
  }

  it("classifies monitorable as free, blocked or needs-llm", async () => {
    const listings = '<script type="application/ld+json">{"@type":"ItemList","itemListElement":[{"@type":"Product","name":"A","url":"/a"}]}</script>';
    const ambiguous = '<script type="application/ld+json">{"@type":"Product","offers":[{"price":"1","priceCurrency":"USD"},{"price":"2","priceCurrency":"USD"}]}</script>';
    assert.equal(await monitorableFor(okPage(html)), "free");
    assert.equal(await monitorableFor(okPage(listings)), "free");
    assert.equal(await monitorableFor({ outcome: "error", kind: "blocked", httpStatus: 403, message: "HTTP 403" }), "blocked");
    assert.equal(await monitorableFor(okPage(ambiguous)), "needs-llm");
    assert.equal(await monitorableFor(okPage("<html>nothing</html>")), "needs-llm");
    assert.equal(await monitorableFor({ outcome: "error", kind: "http", httpStatus: 404, message: "HTTP 404" }), "needs-llm");
    assert.equal(await monitorableFor({ outcome: "error", kind: "timeout", message: "t" }), "needs-llm");
  });

  it("requires exactly one url", async () => {
    const { deps } = createDeps();
    assert.equal((await runCli(["probe"], deps)).exitCode, 2);
    assert.equal((await runCli(["probe", "a", "b"], deps)).exitCode, 2);
  });
});

describe("cli failures", () => {
  it("exits 2 for usage errors", async () => {
    const { deps, calls } = createDeps();
    const cases: string[][] = [
      [],
      ["items"],
      ["items", "explode"],
      ["items", "get"],
      ["items", "get", "a", "b"],
      ["items", "list", "--role", "nonsense"],
      ["items", "list", "--bogus"],
      ["items", "create", "--role", "to-buy", "--name", "x"],
      ["items", "update", "c1"],
      ["items", "move", "c1"],
      ["sources", "add", "c1", "--name", "n"],
      ["notes", "list", "c1", "--limit", "zero"],
    ];
    for (const argv of cases) {
      const result = await runCli(argv, deps);
      const output = parseOutput(result.output);
      assert.equal(result.exitCode, 2, argv.join(" "));
      assert.equal(output.ok, false);
      assert.equal(output.error?.code, "usage");
    }
    assert.equal(calls.length, 0);
  });

  it("exits 1 with the store error code", async () => {
    const { deps } = createDeps();
    const failing: CliDeps = {
      ...deps,
      getStore: async () => ({
        ...(await deps.getStore()),
        getItem: async () => {
          throw new StoreError("not_found", "card missing");
        },
      }),
    };
    const result = await runCli(["items", "get", "nope"], failing);
    assert.equal(result.exitCode, 1);
    assert.deepEqual(parseOutput(result.output), { ok: false, error: { code: "not_found", message: "card missing" } });
  });

  it("exits 1 with a validation error for a bad spec file", async () => {
    const { deps, calls } = createDeps({
      "bad.json": JSON.stringify({ type: "general", condition: "mint" }),
      "typo.json": JSON.stringify({ type: "general", categry: "x" }),
      "garbage.json": "not json",
    });
    for (const file of ["bad.json", "typo.json", "garbage.json"]) {
      const result = await runCli(["items", "create", "--role", "to-buy", "--name", "x", "--spec-file", file], deps);
      assert.equal(result.exitCode, 1, file);
      assert.equal(parseOutput(result.output).error?.code, "validation");
    }
    assert.equal(calls.length, 0);
  });

  it("surfaces config errors from store construction", async () => {
    const { deps } = createDeps();
    const failing: CliDeps = {
      ...deps,
      getStore: async () => {
        throw new StoreError("config", "cannot read config file");
      },
    };
    const result = await runCli(["items", "list"], failing);
    assert.equal(result.exitCode, 1);
    assert.equal(parseOutput(result.output).error?.code, "config");
  });
});

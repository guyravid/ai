import assert from "node:assert/strict";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, it } from "node:test";
import {
  FileStateStore,
  MAX_SEEN_LISTING_IDS,
  StateError,
  emptyItemState,
  resolveStateDir,
  type ItemState,
} from "./state-store.ts";

let directory = "";

beforeEach(async () => {
  directory = await mkdtemp(join(tmpdir(), "deals-state-"));
});

afterEach(async () => {
  await rm(directory, { recursive: true, force: true });
});

function sampleState(): ItemState {
  return {
    schemaVersion: 1,
    sources: {
      "https://a.example/p": {
        etag: '"e"',
        lastModified: "Tue, 01 Oct 2026 10:00:00 GMT",
        hash: "abc",
        lastPrice: 149.99,
        currency: "CAD",
        lastCheckedAt: "2026-10-05T10:00:00.000Z",
        lastChangedAt: "2026-10-04T10:00:00.000Z",
        blockedSince: "2026-10-03T10:00:00.000Z",
        seenListingIds: ["l1", "l2"],
        consecutiveFailures: 2,
      },
    },
    alertedIds: ["l1"],
    lastSweepAt: "2026-10-01T00:00:00.000Z",
    lastRunAt: "2026-10-05T10:00:00.000Z",
  };
}

describe("FileStateStore", () => {
  it("treats a missing file as empty state", async () => {
    assert.deepEqual(await new FileStateStore(directory).load("item1"), emptyItemState());
  });

  it("round-trips state", async () => {
    const store = new FileStateStore(join(directory, "nested"));
    await store.save("item1", sampleState());
    assert.deepEqual(await store.load("item1"), sampleState());
  });

  it("round-trips sparse state with optional fields absent", async () => {
    const store = new FileStateStore(directory);
    const sparse: ItemState = {
      ...emptyItemState(),
      sources: { "https://a.example": { seenListingIds: [], consecutiveFailures: 0 } },
    };
    await store.save("item1", sparse);
    const loaded = await store.load("item1");
    assert.equal(loaded.sources["https://a.example"]?.hash, undefined);
    assert.equal(loaded.sources["https://a.example"]?.consecutiveFailures, 0);
  });

  it("writes atomically, leaving only the final file and a schema version", async () => {
    const store = new FileStateStore(directory);
    await store.save("item1", sampleState());
    await store.save("item1", { ...sampleState(), alertedIds: ["l9"] });
    assert.deepEqual(await readdir(directory), ["item1.json"]);
    const written: unknown = JSON.parse(await readFile(join(directory, "item1.json"), "utf8"));
    assert.deepEqual(written, { ...JSON.parse(JSON.stringify(sampleState())), alertedIds: ["l9"] });
  });

  it("caps seenListingIds at 500, dropping the oldest", async () => {
    const store = new FileStateStore(directory);
    const ids = Array.from({ length: 600 }, (_, index) => `id${index}`);
    const state = sampleState();
    const source = state.sources["https://a.example/p"];
    assert.ok(source !== undefined);
    source.seenListingIds = ids;
    await store.save("item1", state);
    const loaded = (await store.load("item1")).sources["https://a.example/p"]?.seenListingIds ?? [];
    assert.equal(loaded.length, MAX_SEEN_LISTING_IDS);
    assert.equal(loaded[0], "id100");
    assert.equal(loaded.at(-1), "id599");
  });

  it("throws a typed error for corrupt files instead of resetting", async () => {
    await writeFile(join(directory, "bad.json"), "{not json");
    await writeFile(join(directory, "shape.json"), JSON.stringify({ schemaVersion: 1, sources: [], alertedIds: [] }));
    const store = new FileStateStore(directory);
    await assert.rejects(store.load("bad"), { name: "StateError", code: "corrupt" });
    await assert.rejects(store.load("shape"), { code: "corrupt" });
  });

  it("throws on an unknown schemaVersion", async () => {
    await writeFile(join(directory, "v2.json"), JSON.stringify({ schemaVersion: 2, sources: {}, alertedIds: [] }));
    await assert.rejects(new FileStateStore(directory).load("v2"), { code: "unsupported_version" });
  });

  it("rejects unsafe item ids", async () => {
    const store = new FileStateStore(directory);
    for (const id of ["../x", "a/b", ""]) {
      await assert.rejects(store.load(id), (error: unknown) => error instanceof StateError && error.code === "invalid_id");
    }
  });
});

describe("resolveStateDir", () => {
  it("prefers DEALS_STATE_DIR and defaults under the home directory", () => {
    assert.equal(resolveStateDir({ DEALS_STATE_DIR: "/tmp/s" }), "/tmp/s");
    assert.match(resolveStateDir({}), /\.local\/state\/deals$/);
  });
});

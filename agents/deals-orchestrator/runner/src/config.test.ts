import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { describe, it } from "node:test";
import { parseConfig, resolveConfigPath } from "./config.ts";
import { ValidationError } from "./json.ts";

const validConfig = {
  storage: "trello",
  trello: {
    board: "board1",
    lists: { "to-buy": "a", escalate: "b", upgrades: "c", bought: "d", "source-library": null },
  },
};

function withLists(lists: Record<string, unknown>): unknown {
  return { ...validConfig, trello: { board: "board1", lists } };
}

describe("parseConfig", () => {
  it("accepts a valid config with a null source-library", () => {
    const config = parseConfig(validConfig);
    assert.equal(config.trello.board, "board1");
    assert.equal(config.trello.lists["source-library"], null);
    assert.equal(config.trello.lists["to-buy"], "a");
  });

  it("treats a missing source-library as null", () => {
    const config = parseConfig(withLists({ "to-buy": "a", escalate: "b", upgrades: "c", bought: "d" }));
    assert.equal(config.trello.lists["source-library"], null);
  });

  it("accepts a configured source-library", () => {
    const config = parseConfig(
      withLists({ "to-buy": "a", escalate: "b", upgrades: "c", bought: "d", "source-library": "e" }),
    );
    assert.equal(config.trello.lists["source-library"], "e");
  });

  it("rejects bad shapes", () => {
    const bad: unknown[] = [
      null,
      { ...validConfig, storage: "sqlite" },
      { storage: "trello" },
      { storage: "trello", trello: { board: "", lists: {} } },
      withLists({ "to-buy": "a", escalate: "b", upgrades: "c" }),
      withLists({ "to-buy": "a", escalate: "b", upgrades: "c", bought: null }),
      withLists({ "to-buy": "a", escalate: "b", upgrades: "c", bought: "d", wishlist: "x" }),
      withLists({ "to-buy": "a", escalate: "b", upgrades: "c", bought: "d", "source-library": 5 }),
    ];
    for (const candidate of bad) assert.throws(() => parseConfig(candidate), ValidationError);
  });

  it("accepts the shipped example config", async () => {
    const text = await readFile(new URL("../config.example.json", import.meta.url), "utf8");
    assert.equal(parseConfig(JSON.parse(text)).trello.board, "6aa4780bacc867ed68401481");
  });
});

describe("resolveConfigPath", () => {
  it("prefers DEALS_CONFIG", () => {
    assert.equal(resolveConfigPath({ DEALS_CONFIG: "/tmp/x.json" }), "/tmp/x.json");
  });

  it("falls back to the home config", () => {
    assert.match(resolveConfigPath({}), /\.config\/deals\/config\.json$/);
  });
});

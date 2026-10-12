import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, it } from "node:test";
import { ClaudeGate } from "./llm-gate.ts";
import { FileLedger } from "./ledger.ts";
import { acquireLock, withLock } from "./lock.ts";
import { defaultRunConfig } from "./run-config.ts";
import { TelegramSender } from "./telegram.ts";
import { FileTelemetry } from "./telemetry.ts";
import { FakeClaude, RATE_LIMIT_PAYLOAD, MemoryLedger, NOW } from "./test-support.ts";

let directory = "";
beforeEach(async () => {
  directory = await mkdtemp(join(tmpdir(), "deals-run-"));
});
afterEach(async () => {
  await rm(directory, { recursive: true, force: true });
});

describe("FileLedger", () => {
  it("accumulates spend and notices per date", async () => {
    const ledger = new FileLedger(directory);
    assert.equal(await ledger.spentOn("2026-10-05"), 0);
    await ledger.record("2026-10-05", 0.25);
    await ledger.record("2026-10-05", 0.5);
    await ledger.markNotice("2026-10-05", "daily-cap", "t");
    await ledger.record("2026-10-05", 0.25);
    assert.equal(await ledger.spentOn("2026-10-05"), 1);
    assert.equal(await ledger.hasNotice("2026-10-05", "daily-cap"), true);
    assert.equal(await ledger.hasNotice("2026-10-06", "daily-cap"), false);
    assert.equal(await ledger.spentOn("2026-10-06"), 0);
    assert.deepEqual(await readdir(directory), ["ledger-2026-10-05.json"]);
  });

  it("rejects a corrupt ledger", async () => {
    await writeFile(join(directory, "ledger-2026-10-05.json"), "{bad");
    await assert.rejects(new FileLedger(directory).spentOn("2026-10-05"), /corrupt ledger/);
  });
});

describe("lock", () => {
  it("is exclusive while the holder is alive and released afterwards", async () => {
    const alive = { pid: 111, isAlive: () => true };
    const first = await acquireLock(directory, alive);
    assert.ok(first !== null);
    assert.equal(await acquireLock(directory, { pid: 222, isAlive: () => true }), null);
    await first.release();
    assert.ok((await acquireLock(directory, { pid: 222, isAlive: () => true })) !== null);
  });

  it("cleans up a stale lock whose pid is dead", async () => {
    await writeFile(join(directory, "run.lock"), "99999");
    const lock = await acquireLock(directory, { pid: 5, isAlive: () => false });
    assert.ok(lock !== null);
    assert.equal(await readFile(join(directory, "run.lock"), "utf8"), "5");
  });

  it("withLock skips when held and releases after running", async () => {
    await writeFile(join(directory, "run.lock"), "42");
    let ran = false;
    const skipped = await withLock(directory, { pid: 1, isAlive: () => true }, async () => {
      ran = true;
      return "x";
    });
    assert.deepEqual(skipped, { skipped: "locked" });
    assert.equal(ran, false);
    await rm(join(directory, "run.lock"));
    assert.equal(await withLock(directory, { pid: 1, isAlive: () => true }, async () => "done"), "done");
    assert.deepEqual(await readdir(directory), []);
  });

  it("releases the lock when the action throws", async () => {
    await assert.rejects(withLock(directory, { pid: 1, isAlive: () => true }, async () => Promise.reject(new Error("boom"))), /boom/);
    assert.deepEqual(await readdir(directory), []);
  });
});

describe("TelegramSender", () => {
  it("writes the text to a file, sends with --confirm and deletes the file", async () => {
    const seen: { args: string[]; text: string }[] = [];
    const sender = new TelegramSender({
      directory,
      chat: "123",
      exec: async (args) => {
        seen.push({ args, text: await readFile(args[args.indexOf("--text-file") + 1] ?? "", "utf8") });
        return { stdout: JSON.stringify({ ok: true, data: {} }), exitCode: 0 };
      },
    });
    assert.deepEqual(await sender.send("hello"), { ok: true });
    const args = seen[0]?.args ?? [];
    assert.deepEqual([args[0], args[1]], ["messages", "send"]);
    assert.deepEqual(args.slice(-5), ["--parse-mode", "none", "--chat", "123", "--confirm"]);
    assert.equal(seen[0]?.text, "hello");
    assert.deepEqual(await readdir(directory), []);
  });

  it("omits --chat when none is configured and reports envelope errors", async () => {
    let captured: string[] = [];
    const sender = new TelegramSender({
      directory,
      chat: null,
      exec: async (args) => {
        captured = args;
        return { stdout: JSON.stringify({ ok: false, error: { code: "refused", message: "chat unreachable" } }), exitCode: 1 };
      },
    });
    assert.deepEqual(await sender.send("x"), { ok: false, error: "chat unreachable" });
    assert.ok(!captured.includes("--chat"));
    assert.deepEqual(await readdir(directory), []);
  });

  it("reports non-JSON output and thrown errors, still cleaning up", async () => {
    const garbage = new TelegramSender({ directory, chat: null, exec: async () => ({ stdout: "oops", exitCode: 2 }) });
    assert.equal((await garbage.send("x")).ok, false);
    const thrown = new TelegramSender({ directory, chat: null, exec: async () => Promise.reject(new Error("spawn failed")) });
    assert.deepEqual(await thrown.send("x"), { ok: false, error: "spawn failed" });
    assert.deepEqual(await readdir(directory), []);
  });
});

describe("FileTelemetry", () => {
  it("appends one JSON line per call", async () => {
    const sink = new FileTelemetry(join(directory, "logs"));
    await sink.append({ a: 1 });
    await sink.append({ b: 2 });
    const text = await readFile(join(directory, "logs", "runs.jsonl"), "utf8");
    assert.deepEqual(text.trim().split("\n").map((line) => JSON.parse(line)), [{ a: 1 }, { b: 2 }]);
  });
});

describe("ClaudeGate", () => {
  const agent = { name: "deal-searcher", description: "d", tools: ["WebSearch", "WebFetch"], prompt: "PROMPT" };

  function gateWith(claude: FakeClaude, ledger = new MemoryLedger(), dryRun = false): ClaudeGate {
    return new ClaudeGate({ config: defaultRunConfig(), agent, exec: claude.exec, ledger, dryRun });
  }

  it("builds the stripped invocation per mode and records cost", async () => {
    const claude = new FakeClaude();
    const ledger = new MemoryLedger();
    const gate = gateWith(claude, ledger);
    const evaluate = await gate.call("evaluate-listings", { listings: [] }, NOW);
    assert.equal(evaluate.kind, "ok");
    await gate.call("sweep", { spec: {} }, NOW);

    const [first, second] = claude.calls;
    assert.deepEqual(first?.args.slice(-2), ["--tools", ""]);
    assert.equal(first?.args[first.args.indexOf("--model") + 1], "sonnet");
    assert.equal(first?.args[first.args.indexOf("--max-budget-usd") + 1], "0.15");
    assert.equal(first?.args[first.args.indexOf("--system-prompt") + 1], "PROMPT");
    assert.equal(first?.input["mode"], "evaluate-listings");
    assert.deepEqual(second?.args.slice(-2), ["--tools", "WebSearch,WebFetch"]);
    assert.equal(second?.args[second.args.indexOf("--max-budget-usd") + 1], "0.4");
    assert.equal(ledger.records, 2);
    assert.ok(Math.abs(([...ledger.spent.values()][0] ?? 0) - 0.1) < 1e-9);
  });

  it("skips when the daily cap would be exceeded and flags it", async () => {
    const claude = new FakeClaude();
    const ledger = new MemoryLedger();
    const gate = gateWith(claude, ledger);
    await gate.call("evaluate-listings", {}, NOW);
    ledger.spent.set([...ledger.spent.keys()][0] ?? "", 0.9);
    const outcome = await gate.call("evaluate-listings", {}, NOW);
    assert.deepEqual(outcome, { kind: "skipped", reason: "daily-cap" });
    assert.equal(gate.dailyCapHit, true);
    assert.equal(claude.calls.length, 1);
  });

  it("stops all calls after a rate limit and keeps the reset message", async () => {
    const claude = new FakeClaude();
    claude.handler = () => ({ raw: RATE_LIMIT_PAYLOAD });
    const gate = gateWith(claude);
    const first = await gate.call("evaluate-listings", {}, NOW);
    assert.equal(first.kind === "failed" && first.failure.kind, "rate_limited");
    assert.deepEqual(await gate.call("sweep", {}, NOW), { kind: "skipped", reason: "rate-limited" });
    assert.match(gate.rateLimitMessage ?? "", /resets 2:10pm/);
    assert.equal(claude.calls.length, 1);
  });

  it("plans instead of calling in dry-run", async () => {
    const claude = new FakeClaude();
    const ledger = new MemoryLedger();
    const gate = gateWith(claude, ledger, true);
    assert.deepEqual(await gate.call("sweep", {}, NOW), { kind: "skipped", reason: "dry-run" });
    assert.deepEqual(gate.plannedCalls, [{ mode: "sweep", budgetUsd: 0.4 }]);
    assert.equal(claude.calls.length, 0);
    assert.equal(ledger.records, 0);
  });
});

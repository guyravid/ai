import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import { readFile } from "node:fs/promises";
import { loadConfig, resolveConfigPath } from "../config.ts";
import { fetchPage } from "../fetch/fetcher.ts";
import { HostRateLimiter } from "../fetch/rate-limiter.ts";
import { createExecClaude } from "../llm/exec-claude.ts";
import { FileStateStore, resolveStateDir } from "../state/state-store.ts";
import { execTrello } from "../store/exec-trello.ts";
import { TrelloStore } from "../store/trello-store.ts";
import type { RunOptions, RunSummary } from "./deps.ts";
import { FileLedger } from "./ledger.ts";
import { ClaudeGate, loadSearcherAgent } from "./llm-gate.ts";
import { isProcessAlive, withLock } from "./lock.ts";
import { runDeals } from "./runner.ts";
import { TelegramSender } from "./telegram.ts";
import { FileTelemetry, resolveLogDir } from "./telemetry.ts";

function execTelegram(args: string[]): Promise<{ stdout: string; exitCode: number }> {
  return new Promise((resolve) => {
    execFile("telegram", args, { maxBuffer: 4 * 1024 * 1024 }, (error, stdout) => {
      resolve({ stdout, exitCode: error === null ? 0 : typeof error.code === "number" ? error.code : 1 });
    });
  });
}

export async function runWithRealDeps(
  options: RunOptions,
  env: NodeJS.ProcessEnv = process.env,
): Promise<RunSummary | { skipped: "locked" }> {
  const config = await loadConfig(env);
  const stateDir = resolveStateDir(env);
  const ledger = new FileLedger(stateDir);
  const agent = await loadSearcherAgent(config.run.agentsDir, (path) => readFile(path, "utf8"));
  const limiter = new HostRateLimiter();

  return withLock(stateDir, { pid: process.pid, isAlive: isProcessAlive }, () =>
    runDeals(
      {
        config,
        store: new TrelloStore(config.trello, execTrello),
        stateStore: new FileStateStore(stateDir),
        ledger,
        telemetry: new FileTelemetry(resolveLogDir(env)),
        sender: config.run.telegram.enabled
          ? new TelegramSender({ exec: execTelegram, directory: stateDir, chat: config.run.telegram.chat })
          : null,
        gate: new ClaudeGate({ config: config.run, agent, exec: createExecClaude(stateDir), ledger, dryRun: options.dryRun }),
        fetchPage: (url, previous) => fetchPage(url, previous, { limiter }),
        now: () => new Date(),
        runId: randomBytes(4).toString("hex"),
        configPath: resolveConfigPath(env),
      },
      options,
    ),
  );
}

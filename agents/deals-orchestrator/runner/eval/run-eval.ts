#!/usr/bin/env node
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { DEAL_SEARCHER_SCHEMAS } from "../src/llm/schemas.ts";
import { parseArgs } from "node:util";
import {
  formatReport,
  parseAgentFile,
  runEval,
  type ClaudeExec,
  type EvalListing,
  type Label,
} from "./eval-lib.ts";

const AGENT_FILE = new URL("../../.claude/agents/deal-searcher.md", import.meta.url);

const execClaude: ClaudeExec = (args) =>
  new Promise((resolve) => {
    execFile("claude", args, { maxBuffer: 32 * 1024 * 1024 }, (error, stdout) => {
      resolve({ stdout, exitCode: error === null ? 0 : typeof error.code === "number" ? error.code : 1 });
    });
  });

async function readJson(name: string): Promise<unknown> {
  return JSON.parse(await readFile(new URL(name, import.meta.url), "utf8"));
}

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: {
      model: { type: "string", default: "sonnet" },
      "max-budget-usd": { type: "string", default: "0.50" },
      "batch-size": { type: "string", default: "6" },
    },
  });
  const agent = parseAgentFile(await readFile(AGENT_FILE, "utf8"));

  const report = await runEval({
    exec: execClaude,
    listings: (await readJson("listings.json")) as (EvalListing & Record<string, unknown>)[],
    labels: (await readJson("labels.json")) as Record<string, Label>,
    context: (await readJson("context.json")) as { spec: unknown; brief: unknown },
    agentPrompt: agent.prompt,
    tools: [],
    model: values.model ?? "sonnet",
    maxBudgetUsd: Number(values["max-budget-usd"]),
    batchSize: Number(values["batch-size"]),
    jsonSchema: DEAL_SEARCHER_SCHEMAS["evaluate-listings"],
  });
  const thinking = process.env["MAX_THINKING_TOKENS"] ?? "default";
  process.stdout.write(`${formatReport(report, thinking)}\n`);
}

if (import.meta.main) await main();

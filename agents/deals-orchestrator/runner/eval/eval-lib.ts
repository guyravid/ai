import { isObject, type JsonObject } from "../src/json.ts";
import {
  buildClaudeArgs,
  parseClaudeResult,
  totalInputTokens,
  type ClaudeExec,
  type ClaudeUsage,
} from "../src/llm/claude-invocation.ts";

export const VERDICTS = ["match", "uncertain", "reject"] as const;
export type Verdict = (typeof VERDICTS)[number];
export type Predicted = Verdict | "missing";

export { parseAgentFile, type AgentDefinition } from "../src/llm/agent-file.ts";
export type { ClaudeExec, ClaudeUsage };

export interface EvalListing {
  id: string;
}

export interface Label {
  label: Verdict;
  reason: string;
}

export function toBatches<T>(items: T[], size: number): T[][] {
  if (!Number.isInteger(size) || size < 1) throw new Error("batch size must be a positive integer");
  const batches: T[][] = [];
  for (let start = 0; start < items.length; start += size) batches.push(items.slice(start, start + size));
  return batches;
}

export function buildEvaluateInput(
  listings: unknown[],
  context: { spec: unknown; brief: unknown },
): string {
  return JSON.stringify({
    mode: "evaluate-listings",
    brief: context.brief,
    spec: context.spec,
    listings,
    lastNotes: [],
  });
}

export function verdictsFromOutput(parsed: unknown): Map<string, Verdict> {
  const verdicts = new Map<string, Verdict>();
  const matches = isObject(parsed) ? parsed["matches"] : undefined;
  if (!Array.isArray(matches)) return verdicts;
  for (const entry of matches) {
    if (!isObject(entry)) continue;
    const id = entry["listingId"];
    const verdict = VERDICTS.find((candidate) => candidate === entry["verdict"]);
    if (typeof id === "string" && verdict !== undefined) verdicts.set(id, verdict);
  }
  return verdicts;
}

export interface ClassMetrics {
  precision: number | null;
  recall: number | null;
  support: number;
}

export interface Metrics {
  confusion: Record<Verdict, Record<Predicted, number>>;
  perClass: Record<Verdict, ClassMetrics>;
  accuracy: number;
}

function emptyRow(): Record<Predicted, number> {
  return { match: 0, uncertain: 0, reject: 0, missing: 0 };
}

function ratio(numerator: number, denominator: number): number | null {
  return denominator === 0 ? null : numerator / denominator;
}

export function computeMetrics(pairs: { actual: Verdict; predicted: Predicted }[]): Metrics {
  const confusion: Metrics["confusion"] = { match: emptyRow(), uncertain: emptyRow(), reject: emptyRow() };
  for (const { actual, predicted } of pairs) confusion[actual][predicted] += 1;

  const perClass = {} as Metrics["perClass"];
  for (const verdict of VERDICTS) {
    const truePositives = confusion[verdict][verdict];
    const predictedTotal = VERDICTS.reduce((sum, actual) => sum + confusion[actual][verdict], 0);
    const support = Object.values(confusion[verdict]).reduce((sum, count) => sum + count, 0);
    perClass[verdict] = {
      precision: ratio(truePositives, predictedTotal),
      recall: ratio(truePositives, support),
      support,
    };
  }
  const correct = VERDICTS.reduce((sum, verdict) => sum + confusion[verdict][verdict], 0);
  return { confusion, perClass, accuracy: pairs.length === 0 ? 0 : correct / pairs.length };
}

export interface CallRecord {
  batchIndex: number;
  costUsd: number;
  usage: ClaudeUsage;
  error: string | null;
}

export interface EvalReport {
  model: string;
  metrics: Metrics;
  calls: CallRecord[];
  totalCostUsd: number;
}

export async function runEval(options: {
  exec: ClaudeExec;
  listings: (EvalListing & Record<string, unknown>)[];
  labels: Record<string, Label>;
  context: { spec: unknown; brief: unknown };
  agentPrompt: string;
  tools: string[];
  model: string;
  maxBudgetUsd: number;
  batchSize: number;
  jsonSchema?: JsonObject;
}): Promise<EvalReport> {
  const calls: CallRecord[] = [];
  const pairs: { actual: Verdict; predicted: Predicted }[] = [];

  for (const [batchIndex, batch] of toBatches(options.listings, options.batchSize).entries()) {
    const args = buildClaudeArgs({
      prompt: buildEvaluateInput(batch, options.context),
      agentPrompt: options.agentPrompt,
      tools: options.tools,
      model: options.model,
      maxBudgetUsd: options.maxBudgetUsd,
      ...(options.jsonSchema === undefined ? {} : { jsonSchema: options.jsonSchema }),
    });
    let verdicts = new Map<string, Verdict>();
    let record: CallRecord;
    try {
      const executed = await options.exec(args);
      const result = parseClaudeResult(executed.stdout);
      verdicts = verdictsFromOutput(result.output);
      const error =
        result.failure !== null
          ? `${result.failure.kind} (subtype ${result.failure.subtype}): ${result.failure.message}`
          : executed.exitCode !== 0
            ? `exit ${executed.exitCode}`
            : null;
      record = { batchIndex, costUsd: result.costUsd, usage: result.usage, error };
    } catch (error) {
      const message = error instanceof Error ? error.message : "unknown error";
      record = {
        batchIndex,
        costUsd: 0,
        usage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
        error: message,
      };
    }
    calls.push(record);
    for (const listing of batch) {
      const label = options.labels[listing.id];
      if (label !== undefined) pairs.push({ actual: label.label, predicted: verdicts.get(listing.id) ?? "missing" });
    }
  }
  return {
    model: options.model,
    metrics: computeMetrics(pairs),
    calls,
    totalCostUsd: calls.reduce((sum, call) => sum + call.costUsd, 0),
  };
}

function percent(value: number | null): string {
  return value === null ? "n/a" : `${(value * 100).toFixed(0)}%`;
}

export function formatReport(report: EvalReport, thinkingSetting = "default"): string {
  const lines = [`model: ${report.model}`, `thinking (MAX_THINKING_TOKENS): ${thinkingSetting}`, `accuracy: ${percent(report.metrics.accuracy)}`, "", "class       precision  recall  support"];
  for (const verdict of VERDICTS) {
    const entry = report.metrics.perClass[verdict];
    lines.push(`${verdict.padEnd(11)} ${percent(entry.precision).padEnd(10)} ${percent(entry.recall).padEnd(7)} ${entry.support}`);
  }
  lines.push("", "confusion (rows = actual, cols = predicted)", "            match  uncertain  reject  missing");
  for (const verdict of VERDICTS) {
    const row = report.metrics.confusion[verdict];
    lines.push(
      `${verdict.padEnd(11)} ${String(row.match).padEnd(6)} ${String(row.uncertain).padEnd(10)} ${String(row.reject).padEnd(7)} ${row.missing}`,
    );
  }
  lines.push("", "calls:");
  for (const call of report.calls) {
    const { inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens } = call.usage;
    lines.push(
      `  #${call.batchIndex} $${call.costUsd.toFixed(4)} totalIn=${totalInputTokens(call.usage)} (in=${inputTokens} cacheRead=${cacheReadTokens} cacheWrite=${cacheCreationTokens}) out=${outputTokens}${call.error === null ? "" : ` ERROR ${call.error}`}`,
    );
  }
  lines.push(`total cost: $${report.totalCostUsd.toFixed(4)}`);
  return lines.join("\n");
}

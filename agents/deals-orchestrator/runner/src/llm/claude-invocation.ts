import { isObject, type JsonObject } from "../json.ts";

export interface ClaudeUsage {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
}

export type ClaudeFailureKind = "rate_limited" | "budget" | "api_error" | "invalid_output";

export interface ClaudeFailure {
  kind: ClaudeFailureKind;
  subtype: string;
  apiErrorStatus?: number | undefined;
  message: string;
}

export interface ClaudeCallResult {
  text: string;
  subtype: string;
  costUsd: number;
  usage: ClaudeUsage;
  output: unknown;
  failure: ClaudeFailure | null;
}

export interface ExecResult {
  stdout: string;
  exitCode: number;
}

export type ClaudeExec = (args: string[]) => Promise<ExecResult>;

export function totalInputTokens(usage: ClaudeUsage): number {
  return usage.inputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
}

export function buildClaudeArgs(options: {
  prompt: string;
  agentPrompt: string;
  model: string;
  tools: string[];
  maxBudgetUsd: number;
  jsonSchema?: JsonObject;
}): string[] {
  const schemaArgs = options.jsonSchema === undefined ? [] : ["--json-schema", JSON.stringify(options.jsonSchema)];
  return [
    "-p",
    options.prompt,
    "--model",
    options.model,
    "--output-format",
    "json",
    "--max-budget-usd",
    String(options.maxBudgetUsd),
    "--system-prompt",
    options.agentPrompt,
    "--strict-mcp-config",
    "--setting-sources",
    "",
    "--disable-slash-commands",
    "--no-session-persistence",
    "--exclude-dynamic-system-prompt-sections",
    ...schemaArgs,
    "--tools",
    options.tools.join(","),
  ];
}

function numberField(object: JsonObject, key: string): number {
  const value = object[key];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

const RATE_LIMIT_TEXT = /^You've hit your session limit/;
const EMPTY_USAGE: ClaudeUsage = { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 };

function stripFence(text: string): string {
  return text.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "").trim();
}

function classifyFailure(result: JsonObject, text: string, subtype: string): ClaudeFailure | null {
  const status = typeof result["api_error_status"] === "number" ? result["api_error_status"] : undefined;
  const isError = result["is_error"] === true || status !== undefined;
  if (status === 429 || RATE_LIMIT_TEXT.test(text)) {
    return { kind: "rate_limited", subtype, apiErrorStatus: status, message: text };
  }
  if (subtype === "error_max_budget_usd") return { kind: "budget", subtype, apiErrorStatus: status, message: text || subtype };
  if (isError) return { kind: "api_error", subtype, apiErrorStatus: status, message: text || subtype };
  return null;
}

function readOutput(result: JsonObject, text: string): { output: unknown; failure: string | null } {
  const structured = result["structured_output"];
  if (structured !== undefined && structured !== null) return { output: structured, failure: null };
  try {
    return { output: JSON.parse(stripFence(text)), failure: null };
  } catch {
    return { output: undefined, failure: "result is not valid JSON" };
  }
}

function invalid(message: string): ClaudeCallResult {
  return {
    text: "",
    subtype: "unknown",
    costUsd: 0,
    usage: EMPTY_USAGE,
    output: undefined,
    failure: { kind: "invalid_output", subtype: "unknown", message },
  };
}

export function parseClaudeResult(stdout: string): ClaudeCallResult {
  let parsed: unknown;
  try {
    parsed = JSON.parse(stdout);
  } catch {
    return invalid("claude stdout is not JSON");
  }
  const candidates = Array.isArray(parsed) ? parsed : [parsed];
  const result = candidates.find((entry): entry is JsonObject => isObject(entry) && entry["type"] === "result");
  if (result === undefined) return invalid("claude output has no result object");

  const text = typeof result["result"] === "string" ? result["result"] : "";
  const subtype = typeof result["subtype"] === "string" ? result["subtype"] : "unknown";
  const usage = isObject(result["usage"]) ? result["usage"] : {};
  const base = {
    text,
    subtype,
    costUsd: numberField(result, "total_cost_usd"),
    usage: {
      inputTokens: numberField(usage, "input_tokens"),
      outputTokens: numberField(usage, "output_tokens"),
      cacheReadTokens: numberField(usage, "cache_read_input_tokens"),
      cacheCreationTokens: numberField(usage, "cache_creation_input_tokens"),
    },
  };

  const failure = classifyFailure(result, text, subtype);
  if (failure !== null) return { ...base, output: undefined, failure };
  const { output, failure: outputFailure } = readOutput(result, text);
  return {
    ...base,
    output,
    failure: outputFailure === null ? null : { kind: "invalid_output", subtype, message: outputFailure },
  };
}

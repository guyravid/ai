import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildClaudeArgs, parseClaudeResult, totalInputTokens } from "./claude-invocation.ts";

function flagValue(args: string[], flag: string): string | undefined {
  const index = args.indexOf(flag);
  return index < 0 ? undefined : args[index + 1];
}

describe("buildClaudeArgs", () => {
  const args = buildClaudeArgs({
    prompt: '{"mode":"x"}',
    agentPrompt: "You judge deals.",
    model: "sonnet",
    tools: ["WebSearch", "WebFetch"],
    maxBudgetUsd: 0.5,
  });

  it("puts the prompt right after -p", () => {
    assert.deepEqual(args.slice(0, 2), ["-p", '{"mode":"x"}']);
  });

  it("passes model, json output, budget and system prompt", () => {
    assert.equal(flagValue(args, "--model"), "sonnet");
    assert.equal(flagValue(args, "--output-format"), "json");
    assert.equal(flagValue(args, "--max-budget-usd"), "0.5");
    assert.equal(flagValue(args, "--system-prompt"), "You judge deals.");
  });

  it("includes every environment-overhead flag", () => {
    for (const flag of [
      "--strict-mcp-config",
      "--disable-slash-commands",
      "--no-session-persistence",
      "--exclude-dynamic-system-prompt-sections",
    ]) {
      assert.ok(args.includes(flag), flag);
    }
    assert.equal(flagValue(args, "--setting-sources"), "");
  });

  it("never uses --agents or --agent", () => {
    assert.ok(!args.includes("--agents") && !args.includes("--agent"));
  });

  it("keeps --tools last as a single comma-joined value", () => {
    assert.deepEqual(args.slice(-2), ["--tools", "WebSearch,WebFetch"]);
  });

  it("passes an empty tools list as an empty value", () => {
    const none = buildClaudeArgs({ prompt: "p", agentPrompt: "a", model: "m", tools: [], maxBudgetUsd: 1 });
    assert.deepEqual(none.slice(-2), ["--tools", ""]);
  });
});

describe("buildClaudeArgs json schema", () => {
  it("places --json-schema before --tools and only when given", () => {
    const schema = { type: "object", properties: {}, additionalProperties: false };
    const args = buildClaudeArgs({ prompt: "p", agentPrompt: "a", model: "sonnet", tools: [], maxBudgetUsd: 1, jsonSchema: schema });
    const index = args.indexOf("--json-schema");
    assert.equal(args[index + 1], JSON.stringify(schema));
    assert.ok(index < args.indexOf("--tools"));
    assert.deepEqual(args.slice(-2), ["--tools", ""]);
    const without = buildClaudeArgs({ prompt: "p", agentPrompt: "a", model: "sonnet", tools: [], maxBudgetUsd: 1 });
    assert.ok(!without.includes("--json-schema"));
  });
});

describe("parseClaudeResult", () => {
  const success = {
    type: "result",
    subtype: "success",
    is_error: false,
    result: '{"a":1}',
    total_cost_usd: 0.0007,
    usage: { input_tokens: 400, cache_creation_input_tokens: 80, cache_read_input_tokens: 24, output_tokens: 50 },
  };

  it("extracts text, subtype, cost, usage and parsed output", () => {
    const result = parseClaudeResult(JSON.stringify(success));
    assert.equal(result.failure, null);
    assert.deepEqual(result.output, { a: 1 });
    assert.equal(result.costUsd, 0.0007);
    assert.equal(totalInputTokens(result.usage), 504);
  });

  it("prefers structured_output over result text", () => {
    const result = parseClaudeResult(JSON.stringify({ ...success, structured_output: { b: 2 }, result: "ignored" }));
    assert.deepEqual(result.output, { b: 2 });
    assert.equal(result.failure, null);
  });

  it("strips a code fence around JSON text", () => {
    const result = parseClaudeResult(JSON.stringify({ ...success, result: '```json\n{"c":3}\n```' }));
    assert.deepEqual(result.output, { c: 3 });
  });

  it("flags non-JSON text as invalid_output", () => {
    const result = parseClaudeResult(JSON.stringify({ ...success, result: "I think it matches" }));
    assert.equal(result.failure?.kind, "invalid_output");
  });

  it("classifies the real 429 payload as rate_limited despite subtype success", () => {
    const payload = {
      type: "result",
      is_error: true,
      subtype: "success",
      api_error_status: 429,
      result: "You've hit your session limit \u00b7 resets 2:10pm (America/Toronto)",
      terminal_reason: "api_error",
      total_cost_usd: 0,
      usage: { input_tokens: 0, output_tokens: 0 },
    };
    const result = parseClaudeResult(JSON.stringify(payload));
    assert.equal(result.failure?.kind, "rate_limited");
    assert.equal(result.failure?.apiErrorStatus, 429);
    assert.equal(result.failure?.subtype, "success");
    assert.match(result.failure?.message ?? "", /session limit/);
  });

  it("classifies the session-limit text as rate_limited without a status", () => {
    const result = parseClaudeResult(JSON.stringify({ ...success, is_error: true, result: "You've hit your session limit" }));
    assert.equal(result.failure?.kind, "rate_limited");
  });

  it("classifies budget errors", () => {
    const result = parseClaudeResult(JSON.stringify({ ...success, subtype: "error_max_budget_usd", is_error: true, result: "" }));
    assert.equal(result.failure?.kind, "budget");
    assert.equal(result.failure?.subtype, "error_max_budget_usd");
  });

  it("classifies other errors as api_error", () => {
    const result = parseClaudeResult(JSON.stringify({ ...success, is_error: true, api_error_status: 500, result: "boom" }));
    assert.equal(result.failure?.kind, "api_error");
  });

  it("returns invalid_output for malformed stdout and accepts an array form", () => {
    assert.equal(parseClaudeResult("nope").failure?.kind, "invalid_output");
    assert.equal(parseClaudeResult('{"type":"system"}').failure?.kind, "invalid_output");
    assert.equal(parseClaudeResult(JSON.stringify([{ type: "system" }, success])).failure, null);
  });
});

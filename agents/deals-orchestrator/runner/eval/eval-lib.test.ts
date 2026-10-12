import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { describe, it } from "node:test";
import {
  computeMetrics,
  formatReport,
  parseAgentFile,
  verdictsFromOutput,
  runEval,
  toBatches,
  type ClaudeExec,
  type Label,
} from "./eval-lib.ts";

const AGENT_MD = `---
name: deal-searcher
description: Judges deals.
tools: [WebSearch, WebFetch]
---

## Purpose

Body text.
`;

describe("parseAgentFile", () => {
  it("parses frontmatter and body", () => {
    assert.deepEqual(parseAgentFile(AGENT_MD), {
      name: "deal-searcher",
      description: "Judges deals.",
      tools: ["WebSearch", "WebFetch"],
      prompt: "## Purpose\n\nBody text.",
    });
  });

  it("rejects files without frontmatter or required keys", () => {
    assert.throws(() => parseAgentFile("no frontmatter"));
    assert.throws(() => parseAgentFile("---\nname: x\n---\nbody"));
  });

  it("parses the real deal-searcher definition", () => {
    const text = readFileSync(new URL("../../.claude/agents/deal-searcher.md", import.meta.url), "utf8");
    const agent = parseAgentFile(text);
    assert.equal(agent.name, "deal-searcher");
    assert.deepEqual(agent.tools, ["WebSearch", "WebFetch"]);
    assert.ok(agent.prompt.includes("evaluate-listings"));
  });
});

describe("toBatches", () => {
  it("splits into batches with a short tail", () => {
    assert.deepEqual(toBatches([1, 2, 3, 4, 5], 2), [[1, 2], [3, 4], [5]]);
    assert.deepEqual(toBatches([], 3), []);
  });

  it("rejects invalid sizes", () => {
    assert.throws(() => toBatches([1], 0));
  });
});

describe("computeMetrics", () => {
  it("computes confusion, precision, recall and accuracy", () => {
    const metrics = computeMetrics([
      { actual: "match", predicted: "match" },
      { actual: "match", predicted: "uncertain" },
      { actual: "reject", predicted: "match" },
      { actual: "reject", predicted: "reject" },
      { actual: "reject", predicted: "missing" },
    ]);
    assert.equal(metrics.confusion.match.uncertain, 1);
    assert.equal(metrics.confusion.reject.missing, 1);
    assert.deepEqual(metrics.perClass.match, { precision: 0.5, recall: 0.5, support: 2 });
    assert.deepEqual(metrics.perClass.reject, { precision: 1, recall: 1 / 3, support: 3 });
    assert.deepEqual(metrics.perClass.uncertain, { precision: 0, recall: null, support: 0 });
    assert.equal(metrics.accuracy, 0.4);
  });

  it("handles no data", () => {
    assert.equal(computeMetrics([]).accuracy, 0);
  });
});

describe("verdictsFromOutput", () => {
  it("reads valid verdicts and ignores the rest", () => {
    const output = {
      matches: [
        { listingId: "a", verdict: "match" },
        { listingId: "b", verdict: "reject" },
        { listingId: "c", verdict: "maybe" },
      ],
    };
    assert.deepEqual([...verdictsFromOutput(output)], [["a", "match"], ["b", "reject"]]);
    assert.equal(verdictsFromOutput("prose").size, 0);
    assert.equal(verdictsFromOutput(undefined).size, 0);
  });
});

describe("runEval", () => {
  const listings = ["a", "b", "c", "d", "e"].map((id) => ({ id }));
  const labels: Record<string, Label> = {
    a: { label: "match", reason: "" },
    b: { label: "reject", reason: "" },
    c: { label: "uncertain", reason: "" },
    d: { label: "reject", reason: "" },
    e: { label: "match", reason: "" },
  };

  it("batches, calls the injected exec, aggregates cost and scores verdicts", async () => {
    const seen: string[][] = [];
    const exec: ClaudeExec = async (args) => {
      seen.push(args);
      assert.ok(args.includes("--json-schema"));
      const input = JSON.parse(args[1] ?? "{}") as { listings: { id: string }[] };
      const matches = input.listings
        .filter((listing) => listing.id !== "e")
        .map((listing) => ({ listingId: listing.id, verdict: labels[listing.id]?.label ?? "reject" }));
      assert.ok(args.includes("--system-prompt"));
      return {
        exitCode: 0,
        stdout: JSON.stringify({ type: "result", subtype: "success", is_error: false, result: JSON.stringify({ matches }), total_cost_usd: 0.01, usage: { input_tokens: 10, output_tokens: 5 } }),
      };
    };
    const report = await runEval({
      exec, listings, labels, context: { spec: {}, brief: {} },
      agentPrompt: "prompt", tools: [], model: "sonnet", maxBudgetUsd: 1, batchSize: 2, jsonSchema: { type: "object" },
    });
    assert.equal(seen.length, 3);
    assert.equal(report.calls.length, 3);
    assert.ok(Math.abs(report.totalCostUsd - 0.03) < 1e-9);
    assert.equal(report.metrics.confusion.match.missing, 1);
    assert.equal(report.metrics.accuracy, 0.8);
    assert.match(formatReport(report), /total cost: \$0\.0300/);
  });

  it("records failed calls and scores their listings as missing", async () => {
    const exec: ClaudeExec = async () => ({ exitCode: 1, stdout: "garbage" });
    const report = await runEval({
      exec, listings, labels, context: { spec: {}, brief: {} },
      agentPrompt: "prompt", tools: [], model: "sonnet", maxBudgetUsd: 1, batchSize: 5,
    });
    assert.equal(report.calls[0]?.error !== null, true);
    assert.match(formatReport(report, "0"), /ERROR invalid_output/);
    assert.match(formatReport(report, "0"), /thinking \(MAX_THINKING_TOKENS\): 0/);
    assert.equal(report.metrics.accuracy, 0);
  });
});

describe("eval data", () => {
  it("has a label for every listing and a balanced mix", () => {
    const listings = JSON.parse(readFileSync(new URL("./listings.json", import.meta.url), "utf8")) as { id: string }[];
    const labels = JSON.parse(readFileSync(new URL("./labels.json", import.meta.url), "utf8")) as Record<string, Label>;
    assert.equal(listings.length, 30);
    assert.ok(listings.every((listing) => labels[listing.id] !== undefined));
    const counts = new Map<string, number>();
    for (const label of Object.values(labels)) counts.set(label.label, (counts.get(label.label) ?? 0) + 1);
    assert.deepEqual([...counts].sort(), [["match", 8], ["reject", 17], ["uncertain", 5]]);
  });
});

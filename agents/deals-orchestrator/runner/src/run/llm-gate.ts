import { join } from "node:path";
import {
  buildClaudeArgs,
  parseClaudeResult,
  totalInputTokens,
  type ClaudeExec,
  type ClaudeFailure,
  type ClaudeUsage,
} from "../llm/claude-invocation.ts";
import { parseAgentFile, type AgentDefinition } from "../llm/agent-file.ts";
import { DEAL_SEARCHER_SCHEMAS, type SearcherMode } from "../llm/schemas.ts";
import type { Ledger } from "./ledger.ts";
import { localDateKey } from "./ledger.ts";
import type { RunConfig } from "./run-config.ts";

export type SkipReason = "daily-cap" | "rate-limited" | "dry-run";

export type LlmOutcome =
  | { kind: "ok"; output: unknown; costUsd: number; usage: ClaudeUsage }
  | { kind: "skipped"; reason: SkipReason }
  | { kind: "failed"; failure: ClaudeFailure; costUsd: number; usage: ClaudeUsage };

export interface PlannedCall {
  mode: SearcherMode;
  budgetUsd: number;
}

export interface LlmGate {
  call(mode: SearcherMode, input: unknown, now: Date): Promise<LlmOutcome>;
  readonly rateLimitMessage: string | null;
  readonly dailyCapHit: boolean;
  readonly plannedCalls: PlannedCall[];
}

export async function loadSearcherAgent(agentsDir: string, readFile: (path: string) => Promise<string>): Promise<AgentDefinition> {
  return parseAgentFile(await readFile(join(agentsDir, "deal-searcher.md")));
}

export class ClaudeGate implements LlmGate {
  rateLimitMessage: string | null = null;
  dailyCapHit = false;
  readonly plannedCalls: PlannedCall[] = [];

  private readonly config: RunConfig;
  private readonly agent: AgentDefinition;
  private readonly exec: ClaudeExec;
  private readonly ledger: Ledger;
  private readonly dryRun: boolean;

  constructor(options: { config: RunConfig; agent: AgentDefinition; exec: ClaudeExec; ledger: Ledger; dryRun: boolean }) {
    this.config = options.config;
    this.agent = options.agent;
    this.exec = options.exec;
    this.ledger = options.ledger;
    this.dryRun = options.dryRun;
  }

  async call(mode: SearcherMode, input: unknown, now: Date): Promise<LlmOutcome> {
    if (this.rateLimitMessage !== null) return { kind: "skipped", reason: "rate-limited" };

    const budgetUsd = mode === "sweep" ? this.config.perCallBudgetUsd.sweep : this.config.perCallBudgetUsd.default;
    const dateKey = localDateKey(now);
    if ((await this.ledger.spentOn(dateKey)) + budgetUsd > this.config.dailyCapUsd) {
      this.dailyCapHit = true;
      return { kind: "skipped", reason: "daily-cap" };
    }
    if (this.dryRun) {
      this.plannedCalls.push({ mode, budgetUsd });
      return { kind: "skipped", reason: "dry-run" };
    }

    const args = buildClaudeArgs({
      prompt: JSON.stringify({ mode, ...(typeof input === "object" && input !== null ? input : {}) }),
      agentPrompt: this.agent.prompt,
      model: this.config.models[mode],
      tools: mode === "evaluate-listings" ? [] : this.agent.tools,
      maxBudgetUsd: budgetUsd,
      jsonSchema: DEAL_SEARCHER_SCHEMAS[mode],
    });
    const executed = await this.exec(args);
    const result = parseClaudeResult(executed.stdout);
    await this.ledger.record(dateKey, result.costUsd);

    if (result.failure !== null) {
      if (result.failure.kind === "rate_limited") this.rateLimitMessage = result.failure.message;
      return { kind: "failed", failure: result.failure, costUsd: result.costUsd, usage: result.usage };
    }
    return { kind: "ok", output: result.output, costUsd: result.costUsd, usage: result.usage };
  }
}

export function outcomeTokens(outcome: LlmOutcome): { tokensIn: number; tokensOut: number; costUsd: number } {
  if (outcome.kind === "skipped") return { tokensIn: 0, tokensOut: 0, costUsd: 0 };
  return { tokensIn: totalInputTokens(outcome.usage), tokensOut: outcome.usage.outputTokens, costUsd: outcome.costUsd };
}

import { fileURLToPath } from "node:url";
import {
  ValidationError,
  expectBoolean,
  expectNonEmptyString,
  expectNumber,
  expectObject,
  isNullish,
  type JsonObject,
} from "../json.ts";
import type { SearcherMode } from "../llm/schemas.ts";

export type TrackedRole = "to-buy" | "escalate" | "upgrades";
export const TRACKED_ROLE_LIST: readonly TrackedRole[] = ["to-buy", "escalate", "upgrades"];
export const SEARCHER_MODES: readonly SearcherMode[] = ["evaluate-listings", "check-url", "sweep"];

export interface RunConfig {
  cadenceHours: Record<TrackedRole, number>;
  sweepDays: Record<TrackedRole, number>;
  dueToleranceMinutes: number;
  alert: { upgradesTargetRatio: number };
  models: Record<SearcherMode, string>;
  perCallBudgetUsd: { default: number; sweep: number };
  dailyCapUsd: number;
  maxSearchesPerSweep: number;
  listingsPerCall: number;
  maxListingsPerItemPerRun: number;
  maxNewSourcesPerSweep: number;
  agentsDir: string;
  telegram: { enabled: boolean; chat: string | null };
}

export function defaultAgentsDir(): string {
  return fileURLToPath(new URL("../../../.claude/agents/", import.meta.url));
}

export function defaultRunConfig(): RunConfig {
  return {
    cadenceHours: { "to-buy": 24, escalate: 8, upgrades: 72 },
    sweepDays: { "to-buy": 7, escalate: 2, upgrades: 14 },
    dueToleranceMinutes: 30,
    alert: { upgradesTargetRatio: 0.85 },
    models: { "evaluate-listings": "sonnet", "check-url": "sonnet", sweep: "sonnet" },
    perCallBudgetUsd: { default: 0.15, sweep: 0.4 },
    dailyCapUsd: 1,
    maxSearchesPerSweep: 5,
    listingsPerCall: 8,
    maxListingsPerItemPerRun: 24,
    maxNewSourcesPerSweep: 3,
    agentsDir: defaultAgentsDir(),
    telegram: { enabled: true, chat: null },
  };
}

function positive(value: unknown, path: string): number {
  const number = expectNumber(value, path);
  if (number <= 0) throw new ValidationError(`${path} must be positive`);
  return number;
}

function positiveInteger(value: unknown, path: string): number {
  const number = positive(value, path);
  if (!Number.isInteger(number)) throw new ValidationError(`${path} must be an integer`);
  return number;
}

function section(raw: JsonObject, key: string): JsonObject | undefined {
  const value = raw[key];
  return isNullish(value) ? undefined : expectObject(value, `config.run.${key}`);
}

function mergeNumbers<K extends string>(
  defaults: Record<K, number>,
  override: JsonObject | undefined,
  path: string,
  reader: (value: unknown, path: string) => number,
): Record<K, number> {
  if (override === undefined) return defaults;
  for (const key of Object.keys(override)) {
    if (!(key in defaults)) throw new ValidationError(`${path}.${key} is not a known key`);
  }
  const merged = { ...defaults };
  for (const key of Object.keys(defaults) as K[]) {
    if (!isNullish(override[key])) merged[key] = reader(override[key], `${path}.${key}`);
  }
  return merged;
}

function optionalNumber(
  raw: JsonObject,
  key: string,
  fallback: number,
  reader: (value: unknown, path: string) => number,
): number {
  return isNullish(raw[key]) ? fallback : reader(raw[key], `config.run.${key}`);
}

function parseModels(raw: JsonObject | undefined, defaults: RunConfig["models"]): RunConfig["models"] {
  if (raw === undefined) return defaults;
  const models = { ...defaults };
  for (const key of Object.keys(raw)) {
    if (!SEARCHER_MODES.some((mode) => mode === key)) throw new ValidationError(`config.run.models.${key} is not a known mode`);
  }
  for (const mode of SEARCHER_MODES) {
    if (!isNullish(raw[mode])) models[mode] = expectNonEmptyString(raw[mode], `config.run.models.${mode}`);
  }
  return models;
}

export function parseRunConfig(raw: unknown): RunConfig {
  const defaults = defaultRunConfig();
  if (isNullish(raw)) return defaults;
  const run = expectObject(raw, "config.run");

  const alert = section(run, "alert");
  const budget = section(run, "perCallBudgetUsd");
  const telegram = section(run, "telegram");
  const chat = telegram?.["chat"];

  return {
    cadenceHours: mergeNumbers(defaults.cadenceHours, section(run, "cadenceHours"), "config.run.cadenceHours", positive),
    sweepDays: mergeNumbers(defaults.sweepDays, section(run, "sweepDays"), "config.run.sweepDays", positive),
    dueToleranceMinutes: optionalNumber(run, "dueToleranceMinutes", defaults.dueToleranceMinutes, (value, path) => {
      const number = expectNumber(value, path);
      if (number < 0) throw new ValidationError(`${path} must not be negative`);
      return number;
    }),
    alert: {
      upgradesTargetRatio:
        alert === undefined || isNullish(alert["upgradesTargetRatio"])
          ? defaults.alert.upgradesTargetRatio
          : (() => {
              const ratio = expectNumber(alert["upgradesTargetRatio"], "config.run.alert.upgradesTargetRatio");
              if (ratio <= 0 || ratio > 1) throw new ValidationError("config.run.alert.upgradesTargetRatio must be in (0, 1]");
              return ratio;
            })(),
    },
    models: parseModels(section(run, "models"), defaults.models),
    perCallBudgetUsd: mergeNumbers(defaults.perCallBudgetUsd, budget, "config.run.perCallBudgetUsd", positive),
    dailyCapUsd: optionalNumber(run, "dailyCapUsd", defaults.dailyCapUsd, positive),
    maxSearchesPerSweep: optionalNumber(run, "maxSearchesPerSweep", defaults.maxSearchesPerSweep, positiveInteger),
    listingsPerCall: optionalNumber(run, "listingsPerCall", defaults.listingsPerCall, positiveInteger),
    maxListingsPerItemPerRun: optionalNumber(run, "maxListingsPerItemPerRun", defaults.maxListingsPerItemPerRun, positiveInteger),
    maxNewSourcesPerSweep: optionalNumber(run, "maxNewSourcesPerSweep", defaults.maxNewSourcesPerSweep, positiveInteger),
    agentsDir: isNullish(run["agentsDir"]) ? defaults.agentsDir : expectNonEmptyString(run["agentsDir"], "config.run.agentsDir"),
    telegram: {
      enabled: telegram === undefined || isNullish(telegram["enabled"]) ? true : expectBoolean(telegram["enabled"], "config.run.telegram.enabled"),
      chat: isNullish(chat) ? null : expectNonEmptyString(chat, "config.run.telegram.chat"),
    },
  };
}

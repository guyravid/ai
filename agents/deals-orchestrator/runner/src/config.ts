import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import {
  ValidationError,
  expectNonEmptyString,
  expectObject,
  expectOneOf,
  isNullish,
} from "./json.ts";
import { LIST_ROLES, type ListRole } from "./roles.ts";
import { StoreError } from "./store/store-error.ts";

export interface TrelloConfig {
  board: string;
  lists: Record<ListRole, string | null>;
}

export interface DealsConfig {
  storage: "trello";
  trello: TrelloConfig;
}

const REQUIRED_ROLES: readonly ListRole[] = ["to-buy", "escalate", "upgrades", "bought"];

export function parseConfig(raw: unknown): DealsConfig {
  const root = expectObject(raw, "config");
  expectOneOf(root["storage"], ["trello"], "config.storage");
  const trello = expectObject(root["trello"], "config.trello");
  const board = expectNonEmptyString(trello["board"], "config.trello.board");
  const rawLists = expectObject(trello["lists"], "config.trello.lists");

  for (const key of Object.keys(rawLists)) {
    expectOneOf(key, LIST_ROLES, `config.trello.lists key "${key}"`);
  }

  const lists: Record<ListRole, string | null> = {
    "to-buy": null,
    escalate: null,
    upgrades: null,
    bought: null,
    "source-library": null,
  };
  for (const role of LIST_ROLES) {
    const value = rawLists[role];
    const path = `config.trello.lists.${role}`;
    if (REQUIRED_ROLES.includes(role)) {
      lists[role] = expectNonEmptyString(value, path);
    } else if (!isNullish(value)) {
      lists[role] = expectNonEmptyString(value, path);
    }
  }
  return { storage: "trello", trello: { board, lists } };
}

export function resolveConfigPath(env: NodeJS.ProcessEnv): string {
  const override = env["DEALS_CONFIG"];
  if (override !== undefined && override !== "") return override;
  return join(homedir(), ".config", "deals", "config.json");
}

export async function loadConfig(env: NodeJS.ProcessEnv = process.env): Promise<DealsConfig> {
  const path = resolveConfigPath(env);
  let text: string;
  try {
    text = await readFile(path, "utf8");
  } catch {
    throw new StoreError("config", `cannot read config file ${path}`);
  }
  try {
    return parseConfig(JSON.parse(text));
  } catch (error) {
    const reason = error instanceof ValidationError || error instanceof SyntaxError ? error.message : "invalid";
    throw new StoreError("config", `invalid config ${path}: ${reason}`);
  }
}

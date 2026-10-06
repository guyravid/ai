import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { homedir } from "node:os";
import { join } from "node:path";
import {
  ValidationError,
  expectArray,
  expectNumber,
  expectObject,
  expectString,
  expectStringArray,
  isNullish,
  type JsonObject,
} from "../json.ts";

export const MAX_SEEN_LISTING_IDS = 500;
const SCHEMA_VERSION = 1;

export interface SourceState {
  etag?: string | undefined;
  lastModified?: string | undefined;
  hash?: string | undefined;
  lastPrice?: number | undefined;
  currency?: string | undefined;
  lastCheckedAt?: string | undefined;
  lastChangedAt?: string | undefined;
  blockedSince?: string | undefined;
  seenListingIds: string[];
  consecutiveFailures: number;
}

export interface ItemState {
  schemaVersion: 1;
  sources: Record<string, SourceState>;
  alertedIds: string[];
  lastSweepAt?: string | undefined;
  lastRunAt?: string | undefined;
}

export interface StateStore {
  load(itemId: string): Promise<ItemState>;
  save(itemId: string, state: ItemState): Promise<void>;
}

export type StateErrorCode = "corrupt" | "unsupported_version" | "invalid_id" | "io";

export class StateError extends Error {
  readonly code: StateErrorCode;

  constructor(code: StateErrorCode, message: string) {
    super(message);
    this.name = "StateError";
    this.code = code;
  }
}

export function emptySourceState(): SourceState {
  return { seenListingIds: [], consecutiveFailures: 0 };
}

export function emptyItemState(): ItemState {
  return { schemaVersion: SCHEMA_VERSION, sources: {}, alertedIds: [] };
}

export function capSeenListingIds(ids: string[]): string[] {
  return ids.length > MAX_SEEN_LISTING_IDS ? ids.slice(ids.length - MAX_SEEN_LISTING_IDS) : ids;
}

function optionalString(object: JsonObject, key: string, path: string): string | undefined {
  const value = object[key];
  return isNullish(value) ? undefined : expectString(value, `${path}.${key}`);
}

function parseSourceState(raw: unknown, path: string): SourceState {
  const object = expectObject(raw, path);
  const lastPrice = object["lastPrice"];
  return {
    etag: optionalString(object, "etag", path),
    lastModified: optionalString(object, "lastModified", path),
    hash: optionalString(object, "hash", path),
    lastPrice: isNullish(lastPrice) ? undefined : expectNumber(lastPrice, `${path}.lastPrice`),
    currency: optionalString(object, "currency", path),
    lastCheckedAt: optionalString(object, "lastCheckedAt", path),
    lastChangedAt: optionalString(object, "lastChangedAt", path),
    blockedSince: optionalString(object, "blockedSince", path),
    seenListingIds: capSeenListingIds(expectStringArray(object["seenListingIds"], `${path}.seenListingIds`)),
    consecutiveFailures: expectNumber(object["consecutiveFailures"], `${path}.consecutiveFailures`),
  };
}

export function parseItemState(raw: unknown): ItemState {
  const object = expectObject(raw, "state");
  if (object["schemaVersion"] !== SCHEMA_VERSION) {
    throw new StateError("unsupported_version", `unsupported state schemaVersion ${String(object["schemaVersion"])}`);
  }
  const rawSources = expectObject(object["sources"], "state.sources");
  const sources: Record<string, SourceState> = {};
  for (const [url, value] of Object.entries(rawSources)) sources[url] = parseSourceState(value, `state.sources[${url}]`);
  expectArray(object["alertedIds"], "state.alertedIds");
  return {
    schemaVersion: SCHEMA_VERSION,
    sources,
    alertedIds: expectStringArray(object["alertedIds"], "state.alertedIds"),
    lastSweepAt: optionalString(object, "lastSweepAt", "state"),
    lastRunAt: optionalString(object, "lastRunAt", "state"),
  };
}

export function resolveStateDir(env: NodeJS.ProcessEnv): string {
  const override = env["DEALS_STATE_DIR"];
  if (override !== undefined && override !== "") return override;
  return join(homedir(), ".local", "state", "deals");
}

export class FileStateStore implements StateStore {
  private readonly directory: string;

  constructor(directory: string) {
    this.directory = directory;
  }

  async load(itemId: string): Promise<ItemState> {
    const path = this.pathFor(itemId);
    let text: string;
    try {
      text = await readFile(path, "utf8");
    } catch (error) {
      if (isMissingFile(error)) return emptyItemState();
      throw new StateError("io", `cannot read state file ${path}`);
    }
    try {
      return parseItemState(JSON.parse(text));
    } catch (error) {
      if (error instanceof StateError) throw error;
      const reason = error instanceof ValidationError || error instanceof SyntaxError ? error.message : "invalid";
      throw new StateError("corrupt", `corrupt state file ${path}: ${reason}`);
    }
  }

  async save(itemId: string, state: ItemState): Promise<void> {
    const path = this.pathFor(itemId);
    const capped: ItemState = {
      ...state,
      sources: Object.fromEntries(
        Object.entries(state.sources).map(([url, source]) => [
          url,
          { ...source, seenListingIds: capSeenListingIds(source.seenListingIds) },
        ]),
      ),
    };
    const temporaryPath = `${path}.${randomBytes(6).toString("hex")}.tmp`;
    await mkdir(this.directory, { recursive: true });
    await writeFile(temporaryPath, `${JSON.stringify(capped, null, 2)}\n`, "utf8");
    await rename(temporaryPath, path);
  }

  private pathFor(itemId: string): string {
    if (!/^[A-Za-z0-9_-]+$/.test(itemId)) throw new StateError("invalid_id", `invalid item id "${itemId}"`);
    return join(this.directory, `${itemId}.json`);
  }
}

function isMissingFile(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}

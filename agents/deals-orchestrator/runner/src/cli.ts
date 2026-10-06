#!/usr/bin/env node
import { readFile } from "node:fs/promises";
import { parseArgs, type ParseArgsOptionsConfig } from "node:util";
import { loadConfig } from "./config.ts";
import { ValidationError } from "./json.ts";
import { LIST_ROLES, type ListRole } from "./roles.ts";
import { parseBriefJson, parseSpecJson } from "./spec-json.ts";
import { extractPage } from "./extract/extract.ts";
import type { ExtractResult } from "./extract/types.ts";
import { fetchPage, type FetchResult } from "./fetch/fetcher.ts";
import { HostRateLimiter } from "./fetch/rate-limiter.ts";
import { execTrello } from "./store/exec-trello.ts";
import type { ItemChanges, ItemStore } from "./store/item-store.ts";
import { StoreError } from "./store/store-error.ts";
import { TrelloStore } from "./store/trello-store.ts";

export interface CliDeps {
  getStore: () => Promise<ItemStore>;
  readFile: (path: string) => Promise<string>;
  readStdin: () => Promise<string>;
  fetchUrl: (url: string) => Promise<FetchResult>;
}

export interface CliResult {
  exitCode: number;
  output: string;
}

class UsageError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "UsageError";
  }
}

type OptionValue = string | boolean | (string | boolean)[] | undefined;
type OptionValues = Record<string, OptionValue>;

interface CommandContext {
  store: ItemStore;
  values: OptionValues;
  positionals: string[];
  deps: CliDeps;
}

interface CommandDefinition {
  options: ParseArgsOptionsConfig;
  positionals: string[];
  run: (context: CommandContext) => Promise<unknown>;
}

type StandaloneContext = Omit<CommandContext, "store">;

interface StandaloneDefinition {
  options: ParseArgsOptionsConfig;
  positionals: string[];
  run: (context: StandaloneContext) => Promise<unknown>;
}

const DEFAULT_NOTES_LIMIT = 10;

function optionalString(values: OptionValues, name: string): string | undefined {
  const value = values[name];
  return typeof value === "string" ? value : undefined;
}

function requiredString(values: OptionValues, name: string): string {
  const value = optionalString(values, name);
  if (value === undefined || value.trim() === "") throw new UsageError(`--${name} is required`);
  return value;
}

function parseRole(text: string): ListRole {
  const role = LIST_ROLES.find((candidate) => candidate === text);
  if (role === undefined) throw new UsageError(`invalid role "${text}"; expected one of: ${LIST_ROLES.join(", ")}`);
  return role;
}

function requiredRole(values: OptionValues): ListRole {
  return parseRole(requiredString(values, "role"));
}

function roleList(values: OptionValues): ListRole[] {
  const raw = values["role"];
  if (raw === undefined) return [];
  return (Array.isArray(raw) ? raw : [raw]).map((entry) => parseRole(String(entry)));
}

function parseLimit(values: OptionValues): number {
  const text = optionalString(values, "limit");
  if (text === undefined) return DEFAULT_NOTES_LIMIT;
  const limit = Number(text);
  if (!Number.isInteger(limit) || limit < 1) throw new UsageError("--limit must be a positive integer");
  return limit;
}

async function readInput(path: string, deps: CliDeps): Promise<string> {
  return path === "-" ? deps.readStdin() : deps.readFile(path);
}

async function readJsonFile(path: string, deps: CliDeps): Promise<unknown> {
  const text = await readInput(path, deps);
  try {
    return JSON.parse(text);
  } catch {
    throw new ValidationError(`${path} does not contain valid JSON`);
  }
}

async function buildChanges(values: OptionValues, deps: CliDeps): Promise<ItemChanges> {
  const changes: ItemChanges = {};
  const name = optionalString(values, "name");
  const specFile = optionalString(values, "spec-file");
  const briefFile = optionalString(values, "brief-file");
  const notesFile = optionalString(values, "notes-file");
  if (name !== undefined) changes.name = name;
  if (specFile !== undefined) changes.spec = parseSpecJson(await readJsonFile(specFile, deps));
  if (briefFile !== undefined) changes.brief = parseBriefJson(await readJsonFile(briefFile, deps));
  if (notesFile !== undefined) changes.notes = await readInput(notesFile, deps);
  if (Object.keys(changes).length === 0) {
    throw new UsageError("items update needs at least one of --name, --spec-file, --brief-file, --notes-file");
  }
  return changes;
}

const COMMANDS: Record<string, CommandDefinition> = {
  "items list": {
    options: { role: { type: "string", multiple: true } },
    positionals: [],
    run: ({ store, values }) => {
      const roles = roleList(values);
      return store.listItems(roles.length > 0 ? { roles } : undefined);
    },
  },
  "items get": {
    options: {},
    positionals: ["id"],
    run: ({ store, positionals }) => store.getItem(positionals[0] ?? ""),
  },
  "items create": {
    options: {
      role: { type: "string" },
      name: { type: "string" },
      "spec-file": { type: "string" },
    },
    positionals: [],
    run: async ({ store, values, deps }) => {
      const role = requiredRole(values);
      const name = requiredString(values, "name");
      const spec = parseSpecJson(await readJsonFile(requiredString(values, "spec-file"), deps));
      return store.createItem({ role, name, spec });
    },
  },
  "items update": {
    options: {
      name: { type: "string" },
      "spec-file": { type: "string" },
      "brief-file": { type: "string" },
      "notes-file": { type: "string" },
    },
    positionals: ["id"],
    run: async ({ store, values, positionals, deps }) =>
      store.updateItem(positionals[0] ?? "", await buildChanges(values, deps)),
  },
  "items move": {
    options: { role: { type: "string" } },
    positionals: ["id"],
    run: ({ store, values, positionals }) => store.moveItem(positionals[0] ?? "", requiredRole(values)),
  },
  "items validate": {
    options: {},
    positionals: ["id"],
    run: async ({ store, positionals }) => {
      const item = await store.getItem(positionals[0] ?? "");
      return { id: item.id, searchReady: item.searchReady, problems: item.problems };
    },
  },
  "sources list": {
    options: {},
    positionals: ["itemId"],
    run: ({ store, positionals }) => store.listSources(positionals[0] ?? ""),
  },
  "sources add": {
    options: { name: { type: "string" }, url: { type: "string" }, best: { type: "boolean" } },
    positionals: ["itemId"],
    run: ({ store, values, positionals }) =>
      store.addSource(positionals[0] ?? "", {
        name: requiredString(values, "name"),
        url: requiredString(values, "url"),
        best: values["best"] === true,
      }),
  },
  "sources promote": {
    options: {},
    positionals: ["itemId", "sourceId"],
    run: ({ store, positionals }) => store.promoteSource(positionals[0] ?? "", positionals[1] ?? ""),
  },
  "sources remove": {
    options: {},
    positionals: ["itemId", "sourceId"],
    run: async ({ store, positionals }) => {
      await store.removeSource(positionals[0] ?? "", positionals[1] ?? "");
      return { removed: positionals[1] };
    },
  },
  "notes add": {
    options: { text: { type: "string" } },
    positionals: ["itemId"],
    run: async ({ store, values, positionals, deps }) => {
      const textArgument = requiredString(values, "text");
      const text = textArgument === "-" ? await deps.readStdin() : textArgument;
      if (text.trim() === "") throw new UsageError("note text must not be empty");
      return store.addNote(positionals[0] ?? "", text);
    },
  },
  "notes list": {
    options: { limit: { type: "string" } },
    positionals: ["itemId"],
    run: ({ store, values, positionals }) =>
      store.listNotes(positionals[0] ?? "", { limit: parseLimit(values) }),
  },
  "categories sources": {
    options: {},
    positionals: ["category"],
    run: ({ store, positionals }) => store.listCategorySources(positionals[0] ?? ""),
  },
  "categories add-source": {
    options: { name: { type: "string" }, url: { type: "string" } },
    positionals: ["category"],
    run: ({ store, values, positionals }) =>
      store.addCategorySource(positionals[0] ?? "", {
        name: requiredString(values, "name"),
        url: requiredString(values, "url"),
      }),
  },
};

type Monitorable = "free" | "blocked" | "needs-llm";

function classifyMonitorable(fetched: FetchResult, extraction: ExtractResult | null): Monitorable {
  if (fetched.outcome === "error" && fetched.kind === "blocked") return "blocked";
  if (extraction?.kind === "product") return "free";
  if (extraction?.kind === "listings" && extraction.listings.length > 0) return "free";
  return "needs-llm";
}

async function probeUrl(url: string, deps: CliDeps): Promise<unknown> {
  const fetched = await deps.fetchUrl(url);
  if (fetched.outcome !== "ok") {
    return { url, monitorable: classifyMonitorable(fetched, null), fetch: fetched, extraction: null };
  }
  const { body, ...metadata } = fetched;
  const extraction = extractPage(body, fetched.finalUrl);
  return {
    url,
    monitorable: classifyMonitorable(fetched, extraction),
    fetch: { ...metadata, bytes: Buffer.byteLength(body) },
    extraction,
  };
}

const STANDALONE_COMMANDS: Record<string, StandaloneDefinition> = {
  probe: {
    options: {},
    positionals: ["url"],
    run: ({ positionals, deps }) => probeUrl(positionals[0] ?? "", deps),
  },
};

type ResolvedCommand =
  | { kind: "store"; definition: CommandDefinition; rest: string[] }
  | { kind: "standalone"; definition: StandaloneDefinition; rest: string[] };

function resolveCommand(argv: string[]): ResolvedCommand {
  const [group, action, ...rest] = argv;
  const standalone = group === undefined ? undefined : STANDALONE_COMMANDS[group];
  if (group !== undefined && standalone !== undefined) {
    return { kind: "standalone", definition: standalone, rest: argv.slice(1) };
  }
  const definition = group === undefined || action === undefined ? undefined : COMMANDS[`${group} ${action}`];
  if (definition === undefined) {
    const available = [...Object.keys(COMMANDS), ...Object.keys(STANDALONE_COMMANDS)];
    throw new UsageError(`unknown command; available: ${available.join(", ")}`);
  }
  return { kind: "store", definition, rest };
}

function parseCommandArgs(
  definition: { options: ParseArgsOptionsConfig; positionals: string[] },
  rest: string[],
): { values: OptionValues; positionals: string[] } {
  let parsed;
  try {
    parsed = parseArgs({ args: rest, options: definition.options, allowPositionals: true, strict: true });
  } catch (error) {
    throw new UsageError(error instanceof Error ? error.message : "invalid arguments");
  }
  if (parsed.positionals.length !== definition.positionals.length) {
    throw new UsageError(`expected arguments: ${definition.positionals.map((name) => `<${name}>`).join(" ") || "none"}`);
  }
  return { values: parsed.values, positionals: parsed.positionals };
}

function failure(exitCode: number, code: string, message: string): CliResult {
  return { exitCode, output: JSON.stringify({ ok: false, error: { code, message } }) };
}

export async function runCli(argv: string[], deps: CliDeps): Promise<CliResult> {
  try {
    const resolved = resolveCommand(argv);
    const { values, positionals } = parseCommandArgs(resolved.definition, resolved.rest);
    const data =
      resolved.kind === "standalone"
        ? await resolved.definition.run({ values, positionals, deps })
        : await resolved.definition.run({ store: await deps.getStore(), values, positionals, deps });
    return { exitCode: 0, output: JSON.stringify({ ok: true, data }) };
  } catch (error) {
    if (error instanceof UsageError) return failure(2, "usage", error.message);
    if (error instanceof StoreError) return failure(1, error.code, error.message);
    if (error instanceof ValidationError) return failure(1, "validation", error.message);
    return failure(1, "internal", error instanceof Error ? error.message : "unexpected error");
  }
}

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) chunks.push(Buffer.from(chunk));
  return Buffer.concat(chunks).toString("utf8");
}

async function main(): Promise<void> {
  const limiter = new HostRateLimiter();
  const deps: CliDeps = {
    getStore: async () => new TrelloStore((await loadConfig()).trello, execTrello),
    readFile: (path) => readFile(path, "utf8"),
    readStdin,
    fetchUrl: (url) => fetchPage(url, {}, { limiter }),
  };
  const result = await runCli(process.argv.slice(2), deps);
  process.stdout.write(`${result.output}\n`);
  process.exitCode = result.exitCode;
}

if (import.meta.main) await main();

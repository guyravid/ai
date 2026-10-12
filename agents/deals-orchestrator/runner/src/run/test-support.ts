import { StoreError } from "../store/store-error.ts";
import type { FetchResult, PreviousFetch } from "../fetch/fetcher.ts";
import type { ClaudeExec } from "../llm/claude-invocation.ts";
import { emptyGeneralSpec, emptySpecificSpec, type GeneralSpec, type SearchBrief, type Spec } from "../spec.ts";
import { emptyItemState, emptySourceState, type ItemState, type SourceState, type StateStore } from "../state/state-store.ts";
import type {
  Item,
  ItemChanges,
  ItemStore,
  NewItem,
  NewSource,
  Note,
  Source,
} from "../store/item-store.ts";
import type { DealsConfig } from "../config.ts";
import { isSearchReady, validateSpec } from "../spec.ts";
import type { ListRole } from "../roles.ts";
import type { RunDeps } from "./deps.ts";
import type { Ledger } from "./ledger.ts";
import { ClaudeGate } from "./llm-gate.ts";
import { defaultRunConfig, type RunConfig } from "./run-config.ts";
import type { AlertSender, SendResult } from "./telegram.ts";
import type { TelemetrySink } from "./telemetry.ts";

export const NOW = new Date("2026-10-05T15:00:00.000Z");
export const HOUR = 3_600_000;

export function todayKey(): string {
  return `${NOW.getFullYear()}-${String(NOW.getMonth() + 1).padStart(2, "0")}-${String(NOW.getDate()).padStart(2, "0")}`;
}

export function isoAgo(hours: number): string {
  return new Date(NOW.getTime() - hours * HOUR).toISOString();
}

export function generalSpec(overrides: Partial<GeneralSpec> = {}): GeneralSpec {
  return {
    ...emptyGeneralSpec(),
    status: "ready",
    category: "Bikes",
    use: "Trails",
    condition: "used-ok",
    targetPrice: { amount: 150, currency: "CAD" },
    maxPrice: { amount: 200, currency: "CAD" },
    pickupRadiusKm: 50,
    mustHave: ["Disc brakes"],
    ...overrides,
  };
}

export function specificSpec(): Spec {
  return {
    ...emptySpecificSpec(),
    status: "ready",
    condition: "new",
    targetPrice: { amount: 100, currency: "CAD" },
    maxPrice: { amount: 150, currency: "CAD" },
    productName: "Kettle",
    modelNumber: "K1",
  };
}

export function makeItem(options: { id?: string; role?: ListRole; spec?: Spec | null; brief?: SearchBrief | null } = {}): Item {
  const spec = options.spec === undefined ? generalSpec() : options.spec;
  return {
    id: options.id ?? "item1",
    name: `Item ${options.id ?? "item1"}`,
    role: options.role ?? "to-buy",
    url: `https://trello.example/c/${options.id ?? "item1"}`,
    spec,
    brief: options.brief ?? null,
    searchReady: isSearchReady(spec),
    problems: spec === null ? ["no spec"] : validateSpec(spec),
  };
}

export class FakeStore implements ItemStore {
  items: Item[] = [];
  sources = new Map<string, Source[]>();
  notes = new Map<string, Note[]>();
  categorySources = new Map<string, Source[]>();
  libraryConfigured = true;
  failListSourcesFor: string | null = null;
  readonly writes: string[] = [];
  private nextId = 1;

  async listItems(filter?: { roles?: ListRole[] }): Promise<Item[]> {
    const roles = filter?.roles;
    return this.items.filter((item) => roles === undefined || roles.includes(item.role));
  }
  async getItem(id: string): Promise<Item> {
    const item = this.items.find((candidate) => candidate.id === id);
    if (item === undefined) throw new StoreError("not_found", id);
    return item;
  }
  async createItem(_newItem: NewItem): Promise<Item> {
    throw new Error("unused");
  }
  async updateItem(_id: string, _changes: ItemChanges): Promise<Item> {
    throw new Error("unused");
  }
  async moveItem(_id: string, _role: ListRole): Promise<Item> {
    throw new Error("unused");
  }
  async listSources(itemId: string): Promise<Source[]> {
    if (this.failListSourcesFor === itemId) throw new StoreError("upstream", "boom");
    return [...(this.sources.get(itemId) ?? [])];
  }
  async addSource(itemId: string, source: NewSource): Promise<Source> {
    this.writes.push(`addSource:${itemId}:${source.url}`);
    const list = this.sources.get(itemId) ?? [];
    const existing = list.find((candidate) => candidate.url === source.url);
    if (existing !== undefined) return existing;
    const created: Source = { id: `new${this.nextId++}`, name: source.name, url: source.url, best: source.best === true };
    this.sources.set(itemId, [...list, created]);
    return created;
  }
  async promoteSource(itemId: string, sourceId: string): Promise<Source> {
    this.writes.push(`promoteSource:${itemId}:${sourceId}`);
    const list = (this.sources.get(itemId) ?? []).map((source) => ({ ...source, best: source.id === sourceId }));
    this.sources.set(itemId, list);
    const promoted = list.find((source) => source.id === sourceId);
    if (promoted === undefined) throw new StoreError("not_found", sourceId);
    return promoted;
  }
  async removeSource(itemId: string, sourceId: string): Promise<void> {
    this.writes.push(`removeSource:${itemId}:${sourceId}`);
  }
  async addNote(itemId: string, text: string): Promise<Note> {
    this.writes.push(`addNote:${itemId}`);
    const note: Note = { id: `n${this.nextId++}`, date: NOW.toISOString(), text };
    this.notes.set(itemId, [note, ...(this.notes.get(itemId) ?? [])]);
    return note;
  }
  async listNotes(itemId: string, options: { limit: number }): Promise<Note[]> {
    return (this.notes.get(itemId) ?? []).slice(0, options.limit);
  }
  async listCategorySources(category: string): Promise<Source[]> {
    if (!this.libraryConfigured) throw new StoreError("config", "no source-library");
    return this.categorySources.get(category) ?? [];
  }
  async addCategorySource(category: string, source: { name: string; url: string }): Promise<Source> {
    if (!this.libraryConfigured) throw new StoreError("config", "no source-library");
    this.writes.push(`addCategorySource:${category}:${source.url}`);
    const created: Source = { id: `cat${this.nextId++}`, name: source.name, url: source.url, best: false };
    this.categorySources.set(category, [...(this.categorySources.get(category) ?? []), created]);
    return created;
  }

  noteTexts(itemId: string): string[] {
    return (this.notes.get(itemId) ?? []).map((note) => note.text);
  }
}

export class MemoryState implements StateStore {
  readonly states = new Map<string, ItemState>();
  saves = 0;
  async load(itemId: string): Promise<ItemState> {
    return structuredClone(this.states.get(itemId) ?? emptyItemState());
  }
  async save(itemId: string, state: ItemState): Promise<void> {
    this.saves += 1;
    this.states.set(itemId, structuredClone(state));
  }
  seed(itemId: string, state: Partial<ItemState>, sources: Record<string, Partial<SourceState>> = {}): void {
    const full = emptyItemState();
    for (const [url, source] of Object.entries(sources)) full.sources[url] = { ...emptySourceState(), ...source };
    this.states.set(itemId, { ...full, ...state, sources: full.sources });
  }
  get(itemId: string): ItemState {
    const state = this.states.get(itemId);
    if (state === undefined) throw new Error(`no state for ${itemId}`);
    return state;
  }
}

export class MemoryLedger implements Ledger {
  spent = new Map<string, number>();
  notices = new Map<string, Set<string>>();
  records = 0;
  marks = 0;
  async spentOn(dateKey: string): Promise<number> {
    return this.spent.get(dateKey) ?? 0;
  }
  async record(dateKey: string, costUsd: number): Promise<void> {
    this.records += 1;
    this.spent.set(dateKey, (this.spent.get(dateKey) ?? 0) + costUsd);
  }
  async hasNotice(dateKey: string, key: string): Promise<boolean> {
    return this.notices.get(dateKey)?.has(key) ?? false;
  }
  async markNotice(dateKey: string, key: string): Promise<void> {
    this.marks += 1;
    this.notices.set(dateKey, (this.notices.get(dateKey) ?? new Set()).add(key));
  }
}

export class FakeSender implements AlertSender {
  readonly texts: string[] = [];
  failures = 0;
  failNext(count = 1): void {
    this.failures = count;
  }
  async send(text: string): Promise<SendResult> {
    if (this.failures > 0) {
      this.failures -= 1;
      return { ok: false, error: "network down" };
    }
    this.texts.push(text);
    return { ok: true };
  }
}

export class MemoryTelemetry implements TelemetrySink {
  readonly lines: Record<string, unknown>[] = [];
  async append(line: Record<string, unknown>): Promise<void> {
    this.lines.push(line);
  }
}

export interface ClaudeScript {
  output?: unknown;
  costUsd?: number;
  raw?: Record<string, unknown>;
}

export class FakeClaude {
  readonly calls: { args: string[]; input: Record<string, unknown> }[] = [];
  handler: (input: Record<string, unknown>) => ClaudeScript = () => ({ output: { mode: "x", matches: [], newSources: [], notes: [], specSuggestions: [] } });

  readonly exec: ClaudeExec = async (args) => {
    const input = JSON.parse(args[1] ?? "{}") as Record<string, unknown>;
    this.calls.push({ args, input });
    const script = this.handler(input);
    const envelope = script.raw ?? {
      type: "result",
      subtype: "success",
      is_error: false,
      result: "",
      structured_output: script.output,
      total_cost_usd: script.costUsd ?? 0.05,
      usage: { input_tokens: 100, output_tokens: 20, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
    };
    return { stdout: JSON.stringify(envelope), exitCode: 0 };
  };

  callsFor(mode: string): number {
    return this.calls.filter((call) => call.input["mode"] === mode).length;
  }
}

export const RATE_LIMIT_PAYLOAD = {
  type: "result",
  is_error: true,
  subtype: "success",
  api_error_status: 429,
  result: "You've hit your session limit · resets 2:10pm (America/Toronto)",
  terminal_reason: "api_error",
  total_cost_usd: 0,
  usage: { input_tokens: 0, output_tokens: 0 },
};

export class FakeWeb {
  readonly pages = new Map<string, FetchResult>();
  readonly requested: string[] = [];
  fetchPage = async (url: string, _previous: PreviousFetch): Promise<FetchResult> => {
    this.requested.push(url);
    return this.pages.get(url) ?? { outcome: "error", kind: "http", httpStatus: 404, message: "HTTP 404" };
  };
  setHtml(url: string, body: string, hash: string): void {
    this.pages.set(url, { outcome: "ok", body, finalUrl: url, httpStatus: 200, hash });
  }
}

export function productHtml(price: number, currency = "CAD", name = "Kettle"): string {
  return `<script type="application/ld+json">${JSON.stringify({
    "@type": "Product",
    name,
    offers: { "@type": "Offer", price: String(price), priceCurrency: currency },
  })}</script>`;
}

export function listingsHtml(listings: { id: string; price: number }[]): string {
  const elements = listings.map((listing) => ({
    "@type": "ListItem",
    item: {
      "@type": "Product",
      name: `Bike ${listing.id}`,
      url: `https://market.example/l/${listing.id}`,
      offers: { "@type": "Offer", price: String(listing.price), priceCurrency: "CAD" },
    },
  }));
  return `<script type="application/ld+json">${JSON.stringify({ "@type": "ItemList", itemListElement: elements })}</script>`;
}

export interface Harness {
  deps: RunDeps;
  store: FakeStore;
  state: MemoryState;
  ledger: MemoryLedger;
  sender: FakeSender;
  telemetry: MemoryTelemetry;
  claude: FakeClaude;
  web: FakeWeb;
  gate: ClaudeGate;
  nextRun: () => RunDeps;
}

export function createHarness(options: { run?: Partial<RunConfig>; dryRun?: boolean; telegram?: boolean; now?: Date } = {}): Harness {
  const run: RunConfig = { ...defaultRunConfig(), ...options.run };
  const config: DealsConfig = {
    storage: "trello",
    trello: {
      board: "board",
      lists: { "to-buy": "a", escalate: "b", upgrades: "c", bought: "d", "source-library": "e" },
    },
    run,
  };
  const store = new FakeStore();
  const state = new MemoryState();
  const ledger = new MemoryLedger();
  const sender = new FakeSender();
  const telemetry = new MemoryTelemetry();
  const claude = new FakeClaude();
  const web = new FakeWeb();
  const makeGate = (): ClaudeGate =>
    new ClaudeGate({
      config: run,
      agent: { name: "deal-searcher", description: "d", tools: ["WebSearch", "WebFetch"], prompt: "PROMPT" },
      exec: claude.exec,
      ledger,
      dryRun: options.dryRun === true,
    });
  const gate = makeGate();
  const deps: RunDeps = {
    config,
    store,
    stateStore: state,
    ledger,
    telemetry,
    sender: options.telegram === false ? null : sender,
    gate,
    fetchPage: web.fetchPage,
    now: () => options.now ?? NOW,
    runId: "run1",
    configPath: "/cfg/config.json",
  };
  return { deps, store, state, ledger, sender, telemetry, claude, web, gate, nextRun: () => ({ ...deps, gate: makeGate() }) };
}

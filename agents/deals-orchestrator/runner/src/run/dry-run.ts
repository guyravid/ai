import type { ItemStore } from "../store/item-store.ts";
import type { StateStore } from "../state/state-store.ts";
import type { Ledger } from "./ledger.ts";
import type { AlertSender } from "./telegram.ts";
import type { TelemetrySink } from "./telemetry.ts";

export function readOnlyStore(store: ItemStore): ItemStore {
  const unavailable = (): never => {
    throw new Error("not available in dry-run");
  };
  return {
    listItems: (filter) => store.listItems(filter),
    getItem: (id) => store.getItem(id),
    listSources: (itemId) => store.listSources(itemId),
    listNotes: (itemId, options) => store.listNotes(itemId, options),
    listCategorySources: (category) => store.listCategorySources(category),
    createItem: unavailable,
    updateItem: unavailable,
    moveItem: unavailable,
    addSource: async (_itemId, source) => ({ id: "dry-run", name: source.name, url: source.url, best: false }),
    promoteSource: async (_itemId, sourceId) => ({ id: sourceId, name: "", url: "", best: true }),
    removeSource: async () => undefined,
    addNote: async (_itemId, text) => ({ id: "dry-run", date: "", text }),
    addCategorySource: async (_category, source) => ({ id: "dry-run", name: source.name, url: source.url, best: false }),
  };
}

export function readOnlyState(state: StateStore): StateStore {
  return { load: (itemId) => state.load(itemId), save: async () => undefined };
}

export function readOnlyLedger(ledger: Ledger): Ledger {
  return {
    spentOn: (dateKey) => ledger.spentOn(dateKey),
    record: async () => undefined,
    hasNotice: (dateKey, key) => ledger.hasNotice(dateKey, key),
    markNotice: async () => undefined,
  };
}

export const planningSender: AlertSender = { send: async () => ({ ok: true }) };

export const silentTelemetry: TelemetrySink = { append: async () => undefined };

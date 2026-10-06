import type { TrelloConfig } from "../config.ts";
import { ITEM_ROLES, LIST_ROLES, type ListRole } from "../roles.ts";
import { isSearchReady, validateSpec } from "../spec.ts";
import { parseDescription, serializeDescription, emptyLayout } from "../template.ts";
import type {
  Item,
  ItemChanges,
  ItemStore,
  NewItem,
  NewSource,
  Note,
  Source,
} from "./item-store.ts";
import { formatSourceName, parseSourceAttachment } from "./source-names.ts";
import { StoreError } from "./store-error.ts";
import { TrelloClient, type Exec } from "./trello-client.ts";
import {
  parseAttachment,
  parseCard,
  parseComment,
  type CardRecord,
} from "./trello-records.ts";

const CARD_FIELDS = "id,name,desc,idList,url";
const BIG_RESULT_FLAGS = ["--max-string", "200000", "--max-bytes", "8000000"];
const LIST_LIMIT = ["--limit", "1000"];

export class TrelloStore implements ItemStore {
  private readonly config: TrelloConfig;
  private readonly client: TrelloClient;

  constructor(config: TrelloConfig, exec: Exec) {
    this.config = config;
    this.client = new TrelloClient(exec);
  }

  async listItems(filter?: { roles?: ListRole[] }): Promise<Item[]> {
    const roles = [...new Set(filter?.roles ?? ITEM_ROLES)];
    const items: Item[] = [];
    for (const role of roles) {
      const cards = await this.client.readAll(
        ["cards", "list", "--board", this.config.board, "--list-id", this.listIdFor(role)],
        ["--fields", CARD_FIELDS, ...LIST_LIMIT, ...BIG_RESULT_FLAGS],
      );
      items.push(...cards.map((card) => toItem(parseCard(card), role)));
    }
    return items;
  }

  async getItem(id: string): Promise<Item> {
    return this.toItemWithRole(await this.fetchCard(id));
  }

  async createItem(newItem: NewItem): Promise<Item> {
    const description = serializeDescription({
      spec: newItem.spec,
      brief: null,
      notes: "",
      layout: emptyLayout(),
    });
    const created = await this.client.write([
      "cards",
      "create",
      "--list-id",
      this.listIdFor(newItem.role),
      "--name",
      newItem.name,
      "--desc",
      description,
    ]);
    const card = parseCard(created);
    return toItem({ ...card, name: newItem.name, desc: description }, newItem.role);
  }

  async updateItem(id: string, changes: ItemChanges): Promise<Item> {
    if (changes.name !== undefined) {
      await this.client.write(["cards", "rename", id, "--name", changes.name]);
    }
    const card = await this.fetchCard(id);
    if (changes.spec === undefined && changes.brief === undefined && changes.notes === undefined) {
      return this.toItemWithRole(card);
    }
    const document = parseDescription(card.desc);
    if (changes.spec !== undefined) document.spec = changes.spec;
    if (changes.brief !== undefined) document.brief = changes.brief;
    if (changes.notes !== undefined) document.notes = changes.notes;
    const description = serializeDescription(document);
    await this.client.write(["cards", "update", id, "--desc", description]);
    return this.toItemWithRole({ ...card, desc: description });
  }

  async moveItem(id: string, role: ListRole): Promise<Item> {
    await this.client.write(["cards", "move", id, "--list-id", this.listIdFor(role)]);
    return toItem(await this.fetchCard(id), role);
  }

  async listSources(itemId: string): Promise<Source[]> {
    const records = await this.client.readAll(
      ["cards", "attachments", itemId],
      ["--fields", "id,name,url", ...LIST_LIMIT],
    );
    return dedupeByUrl(
      records.flatMap((record) => {
        const source = parseSourceAttachment(parseAttachment(record));
        return source === null ? [] : [source];
      }),
    );
  }

  async addSource(itemId: string, source: NewSource): Promise<Source> {
    const name = source.name.trim();
    const url = source.url.trim();
    if (name === "" || url === "") throw new StoreError("usage", "source name and url are required");

    const existing = await this.listSources(itemId);
    const duplicate = existing.find((candidate) => candidate.url === url);
    if (duplicate !== undefined) return duplicate;

    const best = source.best === true;
    const currentBest = existing.find((candidate) => candidate.best);
    if (best && currentBest !== undefined) await this.replaceAttachment(itemId, currentBest, false);
    return this.attach(itemId, { name, url }, best);
  }

  async promoteSource(itemId: string, sourceId: string): Promise<Source> {
    const sources = await this.listSources(itemId);
    const target = sources.find((candidate) => candidate.id === sourceId);
    if (target === undefined) throw new StoreError("not_found", `source ${sourceId} not found on card ${itemId}`);
    if (target.best) return target;

    const currentBest = sources.find((candidate) => candidate.best);
    if (currentBest !== undefined) await this.replaceAttachment(itemId, currentBest, false);
    return this.replaceAttachment(itemId, target, true);
  }

  async removeSource(itemId: string, sourceId: string): Promise<void> {
    const sources = await this.listSources(itemId);
    if (!sources.some((candidate) => candidate.id === sourceId)) {
      throw new StoreError("not_found", `source ${sourceId} not found on card ${itemId}`);
    }
    await this.client.write(["cards", "detach", itemId, "--attachment", sourceId]);
  }

  async addNote(itemId: string, text: string): Promise<Note> {
    const created = parseComment(await this.client.write(["cards", "comment", itemId, "--text", text]));
    return { id: created.id, date: created.date, text: created.text === "" ? text : created.text };
  }

  async listNotes(itemId: string, options: { limit: number }): Promise<Note[]> {
    const envelope = await this.client.read([
      "cards",
      "comments",
      itemId,
      "--fields",
      "id,date,data.text",
      "--limit",
      String(options.limit),
      "--max-string",
      "20000",
    ]);
    if (envelope.data === null) return [];
    if (!Array.isArray(envelope.data)) throw new StoreError("internal", "trello comments result is not an array");
    return envelope.data.map((raw) => parseComment(raw));
  }

  async listCategorySources(category: string): Promise<Source[]> {
    const cardId = await this.findCategoryCardId(category);
    return cardId === null ? [] : this.listSources(cardId);
  }

  async addCategorySource(category: string, source: { name: string; url: string }): Promise<Source> {
    const existingId = await this.findCategoryCardId(category);
    const cardId = existingId ?? (await this.createCategoryCard(category));
    return this.addSource(cardId, source);
  }

  private listIdFor(role: ListRole): string {
    const listId = this.config.lists[role];
    if (listId === null) throw new StoreError("config", `no Trello list is configured for role "${role}"`);
    return listId;
  }

  private roleForList(listId: string): ListRole {
    const role = LIST_ROLES.find((candidate) => this.config.lists[candidate] === listId);
    if (role === undefined) throw new StoreError("config", `card is in list ${listId}, which has no configured role`);
    return role;
  }

  private toItemWithRole(card: CardRecord): Item {
    return toItem(card, this.roleForList(card.idList));
  }

  private async fetchCard(id: string): Promise<CardRecord> {
    const envelope = await this.client.read(["cards", "get", id, "--fields", CARD_FIELDS, ...BIG_RESULT_FLAGS]);
    return parseCard(envelope.data);
  }

  private async attach(itemId: string, source: { name: string; url: string }, best: boolean): Promise<Source> {
    const name = formatSourceName(source.name, best);
    const created = parseAttachment(
      await this.client.write(["cards", "attach-url", itemId, "--url", source.url, "--name", name]),
    );
    return { id: created.id, name: source.name, url: source.url, best };
  }

  private async replaceAttachment(itemId: string, source: Source, best: boolean): Promise<Source> {
    const replacement = await this.attach(itemId, source, best);
    try {
      await this.client.write(["cards", "detach", itemId, "--attachment", source.id]);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "unknown error";
      throw new StoreError(
        "partial",
        `source "${source.name}" was re-attached as ${replacement.id} but old attachment ${source.id} could not be detached: ${reason}`,
        error instanceof StoreError && error.retriable,
      );
    }
    return replacement;
  }

  private async findCategoryCardId(category: string): Promise<string | null> {
    const wanted = category.trim().toLowerCase();
    const cards = await this.client.readAll(
      ["cards", "list", "--board", this.config.board, "--list-id", this.listIdFor("source-library")],
      ["--fields", "id,name,idList", ...LIST_LIMIT],
    );
    const match = cards.map(parseCard).find((card) => card.name.trim().toLowerCase() === wanted);
    return match?.id ?? null;
  }

  private async createCategoryCard(category: string): Promise<string> {
    const created = await this.client.write([
      "cards",
      "create",
      "--list-id",
      this.listIdFor("source-library"),
      "--name",
      category.trim(),
    ]);
    return parseCard(created).id;
  }
}

function dedupeByUrl(sources: Source[]): Source[] {
  const byUrl = new Map<string, Source>();
  for (const source of sources) {
    const existing = byUrl.get(source.url);
    if (existing === undefined || (source.best && !existing.best)) byUrl.set(source.url, source);
  }
  return [...byUrl.values()];
}

function toItem(card: CardRecord, role: ListRole): Item {
  const { spec, brief } = parseDescription(card.desc);
  return {
    id: card.id,
    name: card.name,
    role,
    url: card.url,
    spec,
    brief,
    searchReady: isSearchReady(spec),
    problems: spec === null ? ["no spec"] : validateSpec(spec),
  };
}

import type { ListRole } from "../roles.ts";
import type { SearchBrief, Spec } from "../spec.ts";

export interface Item {
  id: string;
  name: string;
  role: ListRole;
  url: string;
  spec: Spec | null;
  brief: SearchBrief | null;
  searchReady: boolean;
  problems: string[];
}

export interface Source {
  id: string;
  name: string;
  url: string;
  best: boolean;
}

export interface Note {
  id: string;
  date: string;
  text: string;
}

export interface NewItem {
  role: ListRole;
  name: string;
  spec: Spec;
}

export interface ItemChanges {
  name?: string;
  spec?: Spec;
  brief?: SearchBrief;
  notes?: string;
}

export interface NewSource {
  name: string;
  url: string;
  best?: boolean;
}

export interface ItemStore {
  listItems(filter?: { roles?: ListRole[] }): Promise<Item[]>;
  getItem(id: string): Promise<Item>;
  createItem(newItem: NewItem): Promise<Item>;
  updateItem(id: string, changes: ItemChanges): Promise<Item>;
  moveItem(id: string, role: ListRole): Promise<Item>;

  listSources(itemId: string): Promise<Source[]>;
  addSource(itemId: string, source: NewSource): Promise<Source>;
  promoteSource(itemId: string, sourceId: string): Promise<Source>;
  removeSource(itemId: string, sourceId: string): Promise<void>;

  addNote(itemId: string, text: string): Promise<Note>;
  listNotes(itemId: string, options: { limit: number }): Promise<Note[]>;

  listCategorySources(category: string): Promise<Source[]>;
  addCategorySource(category: string, source: { name: string; url: string }): Promise<Source>;
}

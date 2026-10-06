import { isObject } from "../json.ts";
import { StoreError } from "./store-error.ts";

export interface CardRecord {
  id: string;
  name: string;
  desc: string;
  idList: string;
  url: string;
}

export interface AttachmentRecord {
  id: string;
  name: string;
  url: string;
}

export interface CommentRecord {
  id: string;
  date: string;
  text: string;
}

function requireString(record: Record<string, unknown>, key: string, what: string): string {
  const value = record[key];
  if (typeof value !== "string") throw new StoreError("internal", `trello ${what} is missing "${key}"`);
  return value;
}

function optionalString(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === "string" ? value : "";
}

export function parseCard(raw: unknown): CardRecord {
  if (!isObject(raw)) throw new StoreError("internal", "trello card is not an object");
  return {
    id: requireString(raw, "id", "card"),
    name: optionalString(raw, "name"),
    desc: optionalString(raw, "desc"),
    idList: requireString(raw, "idList", "card"),
    url: optionalString(raw, "url"),
  };
}

export function parseAttachment(raw: unknown): AttachmentRecord {
  if (!isObject(raw)) throw new StoreError("internal", "trello attachment is not an object");
  return {
    id: requireString(raw, "id", "attachment"),
    name: optionalString(raw, "name"),
    url: optionalString(raw, "url"),
  };
}

export function parseComment(raw: unknown): CommentRecord {
  if (!isObject(raw)) throw new StoreError("internal", "trello comment is not an object");
  const data = raw["data"];
  return {
    id: requireString(raw, "id", "comment"),
    date: optionalString(raw, "date"),
    text: isObject(data) ? optionalString(data, "text") : "",
  };
}

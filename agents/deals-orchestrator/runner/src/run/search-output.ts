import { isObject } from "../json.ts";
import type { Evidence } from "./alerts.ts";

export type SearchVerdict = "match" | "uncertain" | "reject";

export interface SearchMatch {
  listingId?: string | undefined;
  url?: string | undefined;
  verdict: SearchVerdict;
  price?: number | undefined;
  currency?: string | undefined;
  reasons: string[];
  evidence: Evidence;
  asOf?: string | undefined;
}

export interface NewSourceSuggestion {
  name: string;
  url: string;
  kind: "product" | "search";
  note: string;
}

export interface SearchOutput {
  matches: SearchMatch[];
  newSources: NewSourceSuggestion[];
  notes: string[];
  specSuggestions: string[];
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function readMatch(raw: unknown): SearchMatch | null {
  if (!isObject(raw)) return null;
  const verdict = (["match", "uncertain", "reject"] as const).find((candidate) => candidate === raw["verdict"]);
  if (verdict === undefined) return null;
  const price = raw["price"];
  return {
    listingId: text(raw["listingId"]),
    url: text(raw["url"]),
    verdict,
    price: typeof price === "number" && Number.isFinite(price) ? price : undefined,
    currency: text(raw["currency"])?.toUpperCase(),
    reasons: Array.isArray(raw["reasons"]) ? raw["reasons"].filter((reason): reason is string => typeof reason === "string") : [],
    evidence: raw["evidence"] === "search" ? "search" : "page",
    asOf: text(raw["asOf"]),
  };
}

function readNewSource(raw: unknown): NewSourceSuggestion | null {
  if (!isObject(raw)) return null;
  const name = text(raw["name"]);
  const url = text(raw["url"]);
  if (name === undefined || url === undefined) return null;
  return { name, url, kind: raw["kind"] === "search" ? "search" : "product", note: text(raw["note"]) ?? "" };
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === "string") : [];
}

export function parseSearchOutput(output: unknown): SearchOutput | null {
  if (!isObject(output) || !Array.isArray(output["matches"])) return null;
  return {
    matches: output["matches"].flatMap((entry) => readMatch(entry) ?? []),
    newSources: Array.isArray(output["newSources"]) ? output["newSources"].flatMap((entry) => readNewSource(entry) ?? []) : [],
    notes: strings(output["notes"]),
    specSuggestions: strings(output["specSuggestions"]),
  };
}

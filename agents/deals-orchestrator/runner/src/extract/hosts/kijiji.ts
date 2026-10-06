import { isObject, type JsonObject } from "../../json.ts";
import { normalizeUrl } from "../html.ts";
import type { ExtractResult, Listing } from "../types.ts";

const SNIPPET_MAX_CHARS = 300;
const LISTING_KEY = /^\w*Listing:\d+$/;
const NEXT_DATA = /<script\b[^>]*\bid\s*=\s*["']__NEXT_DATA__["'][^>]*>([\s\S]*?)<\/script>/i;

function readApolloState(html: string): JsonObject | null {
  const block = NEXT_DATA.exec(html)?.[1];
  if (block === undefined) return null;
  try {
    const root: unknown = JSON.parse(block);
    const props = isObject(root) ? root["props"] : undefined;
    const pageProps = isObject(props) ? props["pageProps"] : undefined;
    const state = isObject(pageProps) ? pageProps["__APOLLO_STATE__"] : undefined;
    return isObject(state) ? state : null;
  } catch {
    return null;
  }
}

function text(value: unknown): string | undefined {
  if (typeof value === "string" && value.trim() !== "") return value.trim();
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  return undefined;
}

function readPrice(value: unknown): number | undefined {
  if (!isObject(value)) return undefined;
  if (value["type"] === "FREE") return 0;
  const amount = value["amount"];
  if (value["type"] === "FIXED" && typeof amount === "number" && Number.isFinite(amount) && amount > 0) {
    return amount / 100;
  }
  return undefined;
}

function readSnippet(value: unknown): string | undefined {
  const description = text(value)?.replace(/\s+/g, " ");
  return description === undefined ? undefined : description.slice(0, SNIPPET_MAX_CHARS);
}

interface RawListing {
  organic: boolean;
  listing: Listing;
}

function readListing(entry: JsonObject, pageUrl: string): RawListing | null {
  const id = text(entry["id"]);
  const title = text(entry["title"]);
  const rawUrl = text(entry["url"]);
  const url = rawUrl === undefined ? null : normalizeUrl(rawUrl, pageUrl);
  if (id === undefined || title === undefined || url === null) return null;
  const location = isObject(entry["location"]) ? text(entry["location"]["name"]) : undefined;
  return {
    organic: entry["adSource"] === "ORGANIC",
    listing: {
      id,
      url,
      title,
      price: readPrice(entry["price"]),
      currency: "CAD",
      snippet: readSnippet(entry["description"]),
      location,
      postedAt: text(entry["activationDate"]) ?? text(entry["sortingDate"]),
    },
  };
}

export function extractKijiji(html: string, pageUrl: string): ExtractResult | null {
  const state = readApolloState(html);
  if (state === null) return null;

  const byId = new Map<string, RawListing>();
  for (const [key, value] of Object.entries(state)) {
    if (!LISTING_KEY.test(key) || !isObject(value)) continue;
    const raw = readListing(value, pageUrl);
    if (raw === null) continue;
    const existing = byId.get(raw.listing.id);
    if (existing === undefined || (raw.organic && !existing.organic)) byId.set(raw.listing.id, raw);
  }
  return { kind: "listings", listings: [...byId.values()].map((raw) => raw.listing) };
}

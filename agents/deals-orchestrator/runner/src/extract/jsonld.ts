import { isObject, type JsonObject } from "../json.ts";
import { decodeEntities, normalizeUrl, parsePrice } from "./html.ts";
import {
  distinctCandidates,
  type ExtractResult,
  type Listing,
  type PriceCandidate,
} from "./types.ts";

const IDENTIFIER_KEYS = ["sku", "gtin", "gtin12", "gtin13", "mpn"];

export function extractJsonLdBlocks(html: string): unknown[] {
  const pattern = /<script\b[^>]*type\s*=\s*["']?application\/ld\+json["']?[^>]*>([\s\S]*?)<\/script>/gi;
  const blocks: unknown[] = [];
  for (const match of html.matchAll(pattern)) {
    try {
      blocks.push(JSON.parse((match[1] ?? "").trim()));
    } catch {
      continue;
    }
  }
  return blocks;
}

function flattenNodes(value: unknown): JsonObject[] {
  if (Array.isArray(value)) return value.flatMap(flattenNodes);
  if (!isObject(value)) return [];
  const graph = value["@graph"];
  return [value, ...(graph === undefined ? [] : flattenNodes(graph))];
}

function hasType(node: JsonObject, typeName: string): boolean {
  const type = node["@type"];
  const types = Array.isArray(type) ? type : [type];
  return types.some((entry) => typeof entry === "string" && entry.toLowerCase() === typeName.toLowerCase());
}

function text(value: unknown): string | undefined {
  if (typeof value === "string" && value.trim() !== "") return decodeEntities(value.trim());
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  return undefined;
}

function toArray(value: unknown): unknown[] {
  if (value === undefined || value === null) return [];
  return Array.isArray(value) ? value : [value];
}

function lastPathSegment(value: string): string {
  return value.split("/").pop() ?? value;
}

interface OfferReading {
  candidate: PriceCandidate | null;
  availability: string | undefined;
}

function readOffer(offer: unknown): OfferReading[] {
  if (!isObject(offer)) return [];
  const specification = isObject(offer["priceSpecification"]) ? offer["priceSpecification"] : {};
  const rawPrice = hasType(offer, "AggregateOffer")
    ? offer["lowPrice"] ?? offer["price"]
    : offer["price"] ?? specification["price"];
  const price = parsePrice(rawPrice);
  const currency = text(offer["priceCurrency"] ?? specification["priceCurrency"]);
  const availability = text(offer["availability"]);
  return [
    {
      candidate: price === null || currency === undefined ? null : { price, currency: currency.toUpperCase() },
      availability: availability === undefined ? undefined : lastPathSegment(availability),
    },
  ];
}

function readOffers(node: JsonObject): OfferReading[] {
  return toArray(node["offers"]).flatMap(readOffer);
}

function readIdentifiers(node: JsonObject): Record<string, string> {
  const identifiers: Record<string, string> = {};
  for (const key of IDENTIFIER_KEYS) {
    const value = text(node[key]);
    if (value !== undefined) identifiers[key] = value;
  }
  return identifiers;
}

function readListing(entry: unknown, pageUrl: string): Listing | null {
  if (!isObject(entry)) return null;
  const item = isObject(entry["item"]) ? entry["item"] : entry;
  const rawUrl = text(item["url"] ?? item["@id"] ?? entry["url"]);
  const title = text(item["name"] ?? entry["name"]);
  const url = rawUrl === undefined ? null : normalizeUrl(rawUrl, pageUrl);
  if (url === null || title === undefined) return null;
  const candidates = distinctCandidates(readOffers(item).flatMap((offer) => (offer.candidate ? [offer.candidate] : [])));
  const only = candidates.length === 1 ? candidates[0] : undefined;
  return { id: url, url, title, price: only?.price, currency: only?.currency };
}

function extractListings(nodes: JsonObject[], pageUrl: string): Listing[] {
  const byId = new Map<string, Listing>();
  for (const list of nodes.filter((node) => hasType(node, "ItemList"))) {
    for (const entry of toArray(list["itemListElement"])) {
      const listing = readListing(entry, pageUrl);
      if (listing !== null && !byId.has(listing.id)) byId.set(listing.id, listing);
    }
  }
  return [...byId.values()];
}

export function extractJsonLd(html: string, pageUrl: string): ExtractResult | null {
  const nodes = extractJsonLdBlocks(html).flatMap(flattenNodes);
  const products = nodes.filter((node) => hasType(node, "Product"));

  if (products.length > 0) return extractProduct(products);

  const listings = extractListings(nodes, pageUrl);
  return listings.length > 0 ? { kind: "listings", listings } : null;
}

function extractProduct(products: JsonObject[]): ExtractResult | null {
  const readings = products.flatMap((product) => readOffers(product).map((offer) => ({ product, offer })));
  const candidates = distinctCandidates(readings.flatMap(({ offer }) => (offer.candidate ? [offer.candidate] : [])));
  if (candidates.length === 0) return null;
  if (candidates.length > 1) return { kind: "unextractable", reason: "ambiguous" };

  const [candidate] = candidates;
  const match = readings.find(({ offer }) => offer.candidate !== null);
  if (candidate === undefined || match === undefined) return null;
  return {
    kind: "product",
    price: candidate.price,
    currency: candidate.currency,
    confidence: "high",
    method: "json-ld",
    name: text(match.product["name"]),
    identifiers: readIdentifiers(match.product),
    availability: match.offer.availability,
  };
}

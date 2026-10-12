import { extractKijiji } from "./hosts/kijiji.ts";
import { extractJsonLd } from "./jsonld.ts";
import { extractMicrodata } from "./microdata.ts";
import { extractOpenGraph } from "./opengraph.ts";
import {
  distinctCandidates,
  type ExtractMethod,
  type ExtractResult,
  type PriceCandidate,
} from "./types.ts";

export type HostExtractor = (html: string, url: string) => ExtractResult | null;

export const hostExtractors = new Map<string, HostExtractor>([
  ["www.kijiji.ca", extractKijiji],
  ["kijiji.ca", extractKijiji],
]);

function hostOf(url: string): string | null {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return null;
  }
}

function fromCandidates(
  candidates: PriceCandidate[],
  method: ExtractMethod,
  name: string | undefined,
): ExtractResult | null {
  const distinct = distinctCandidates(candidates);
  const [only] = distinct;
  if (only === undefined) return null;
  if (distinct.length > 1) return { kind: "unextractable", reason: "ambiguous" };
  return {
    kind: "product",
    price: only.price,
    currency: only.currency,
    confidence: "medium",
    method,
    name,
    identifiers: {},
  };
}

export function extractPage(
  html: string,
  url: string,
  registry: ReadonlyMap<string, HostExtractor> = hostExtractors,
): ExtractResult {
  const host = hostOf(url);
  const hostResult = host === null ? null : (registry.get(host)?.(html, url) ?? null);
  if (hostResult !== null) return hostResult;

  const jsonLd = extractJsonLd(html, url);
  if (jsonLd !== null) return jsonLd;

  const openGraph = extractOpenGraph(html);
  if (openGraph !== null) return fromCandidates(openGraph.candidates, "opengraph", openGraph.name) ?? unextractable();

  return fromCandidates(extractMicrodata(html), "microdata", undefined) ?? unextractable();
}

function unextractable(): ExtractResult {
  return { kind: "unextractable", reason: "no structured price data" };
}

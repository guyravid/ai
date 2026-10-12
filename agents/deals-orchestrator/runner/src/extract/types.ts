export type Confidence = "high" | "medium";
export type ExtractMethod = "json-ld" | "opengraph" | "microdata" | "host";

export interface ProductExtraction {
  kind: "product";
  price: number;
  currency: string;
  confidence: Confidence;
  method: ExtractMethod;
  name?: string | undefined;
  identifiers: Record<string, string>;
  availability?: string | undefined;
}

export interface Listing {
  id: string;
  url: string;
  title: string;
  price?: number | undefined;
  currency?: string | undefined;
  snippet?: string | undefined;
  location?: string | undefined;
  postedAt?: string | undefined;
}

export interface ListingsExtraction {
  kind: "listings";
  listings: Listing[];
}

export interface Unextractable {
  kind: "unextractable";
  reason: string;
}

export type ExtractResult = ProductExtraction | ListingsExtraction | Unextractable;

export interface PriceCandidate {
  price: number;
  currency: string;
}

export function distinctCandidates(candidates: PriceCandidate[]): PriceCandidate[] {
  const seen = new Map<string, PriceCandidate>();
  for (const candidate of candidates) seen.set(`${candidate.price}|${candidate.currency}`, candidate);
  return [...seen.values()];
}

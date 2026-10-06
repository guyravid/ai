import { findTags, parsePrice } from "./html.ts";
import type { PriceCandidate } from "./types.ts";

export interface MetaReading {
  candidates: PriceCandidate[];
  name: string | undefined;
}

function metaContent(html: string, keys: string[]): string[] {
  return findTags(html, "meta").flatMap((tag) => {
    const key = (tag.attributes.get("property") ?? tag.attributes.get("name") ?? "").toLowerCase();
    const content = tag.attributes.get("content");
    return keys.includes(key) && content !== undefined ? [content.trim()] : [];
  });
}

export function candidatesFromAmountsAndCurrencies(amounts: string[], currencies: string[]): PriceCandidate[] {
  const distinctCurrencies = [...new Set(currencies.map((currency) => currency.toUpperCase()).filter((c) => c !== ""))];
  const [currency] = distinctCurrencies;
  if (distinctCurrencies.length !== 1 || currency === undefined) return [];
  return amounts.flatMap((amount) => {
    const price = parsePrice(amount);
    return price === null ? [] : [{ price, currency }];
  });
}

export function extractOpenGraph(html: string): MetaReading | null {
  const amounts = metaContent(html, ["product:price:amount", "og:price:amount"]);
  const currencies = metaContent(html, ["product:price:currency", "og:price:currency"]);
  const candidates = candidatesFromAmountsAndCurrencies(amounts, currencies);
  if (candidates.length === 0) return null;
  const title = metaContent(html, ["og:title"])[0];
  return { candidates, name: title === undefined || title === "" ? undefined : title };
}

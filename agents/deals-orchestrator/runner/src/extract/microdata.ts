import { decodeEntities, findTags } from "./html.ts";
import { candidatesFromAmountsAndCurrencies } from "./opengraph.ts";
import type { PriceCandidate } from "./types.ts";

function itempropValues(html: string, property: string): string[] {
  return findTags(html, "[a-z0-9]+").flatMap((tag) => {
    if ((tag.attributes.get("itemprop") ?? "").toLowerCase() !== property.toLowerCase()) return [];
    const content = tag.attributes.get("content");
    if (content !== undefined) return [content.trim()];
    const following = /^[^<]*/.exec(html.slice(tag.end))?.[0] ?? "";
    return [decodeEntities(following).trim()];
  });
}

export function extractMicrodata(html: string): PriceCandidate[] {
  return candidatesFromAmountsAndCurrencies(itempropValues(html, "price"), itempropValues(html, "priceCurrency"));
}

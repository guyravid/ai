import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { describe, it } from "node:test";
import { extractPage, type HostExtractor } from "./extract.ts";
import { decodeEntities, normalizeUrl, parsePrice } from "./html.ts";
import { extractJsonLdBlocks } from "./jsonld.ts";

const URL_BASE = "https://shop.example.com/p/1";

function fixture(name: string): string {
  return readFileSync(new URL(`./fixtures/${name}`, import.meta.url), "utf8");
}

function product(name: string) {
  const result = extractPage(fixture(name), URL_BASE, new Map());
  assert.equal(result.kind, "product", name);
  return result.kind === "product" ? result : (undefined as never);
}

describe("JSON-LD", () => {
  it("reads a Product with an Offer", () => {
    assert.deepEqual(product("jsonld-offer.html"), {
      kind: "product",
      price: 149.99,
      currency: "CAD",
      confidence: "high",
      method: "json-ld",
      name: "Trail Bike & Helmet Set",
      identifiers: { sku: "TB-100", gtin13: "0123456789012", mpn: "MPN-77" },
      availability: "InStock",
    });
  });

  it("finds a Product inside @graph with a multi-type @type and numeric price", () => {
    const result = product("jsonld-graph.html");
    assert.equal(result.price, 1299);
    assert.equal(result.currency, "USD");
    assert.equal(result.identifiers["gtin"], "00012345678905");
  });

  it("uses lowPrice for an AggregateOffer", () => {
    const result = product("jsonld-aggregate.html");
    assert.equal(result.price, 89);
    assert.equal(result.confidence, "high");
  });

  it("accepts an Offer array when all offers agree", () => {
    assert.equal(product("jsonld-offer-array.html").price, 39.99);
  });

  it("parses string prices with symbols and commas", () => {
    assert.equal(product("jsonld-string-price.html").price, 1299.99);
  });

  it("returns ambiguous for conflicting offers", () => {
    assert.deepEqual(extractPage(fixture("ambiguous.html"), URL_BASE, new Map()), {
      kind: "unextractable",
      reason: "ambiguous",
    });
  });

  it("survives a malformed block next to a valid one", () => {
    assert.equal(extractJsonLdBlocks(fixture("malformed-jsonld.html")).length, 1);
    assert.equal(product("malformed-jsonld.html").price, 75);
  });

  it("does not fall back to other methods when JSON-LD is ambiguous", () => {
    const html = `${fixture("ambiguous.html")}<meta property="og:price:amount" content="1"><meta property="og:price:currency" content="USD">`;
    assert.equal(extractPage(html, URL_BASE, new Map()).kind, "unextractable");
  });
});

describe("listings", () => {
  it("extracts ItemList entries with normalized, de-duplicated ids", () => {
    const result = extractPage(fixture("itemlist-search.html"), "https://shop.example.com/search?q=bike", new Map());
    assert.equal(result.kind, "listings");
    if (result.kind !== "listings") return;
    assert.deepEqual(result.listings, [
      {
        id: "https://shop.example.com/listing/1?id=9",
        url: "https://shop.example.com/listing/1?id=9",
        title: "Mountain Bike A",
        price: 250,
        currency: "CAD",
      },
      {
        id: "https://shop.example.com/listing/2",
        url: "https://shop.example.com/listing/2",
        title: "Mountain Bike B",
        price: undefined,
        currency: undefined,
      },
      {
        id: "https://shop.example.com/listing/3",
        url: "https://shop.example.com/listing/3",
        title: "Mountain Bike C",
        price: undefined,
        currency: undefined,
      },
    ]);
  });
});

describe("OpenGraph, microdata and fallbacks", () => {
  it("reads OpenGraph price metadata at medium confidence", () => {
    const result = product("opengraph-only.html");
    assert.deepEqual([result.price, result.currency, result.confidence, result.method, result.name], [
      24.5,
      "GBP",
      "medium",
      "opengraph",
      "Desk Lamp",
    ]);
  });

  it("reads microdata price and currency", () => {
    const result = product("microdata-only.html");
    assert.deepEqual([result.price, result.currency, result.confidence, result.method], [59, "CAD", "medium", "microdata"]);
  });

  it("treats conflicting microdata prices as ambiguous", () => {
    const html = '<meta itemprop="priceCurrency" content="USD"><span itemprop="price">10</span><span itemprop="price">12</span>';
    assert.deepEqual(extractPage(html, URL_BASE, new Map()), { kind: "unextractable", reason: "ambiguous" });
  });

  it("does not guess a price without a currency", () => {
    const html = '<span itemprop="price">10</span>';
    assert.equal(extractPage(html, URL_BASE, new Map()).kind, "unextractable");
  });

  it("reports unextractable for a page without structured data", () => {
    assert.deepEqual(extractPage(fixture("no-structured.html"), URL_BASE, new Map()), {
      kind: "unextractable",
      reason: "no structured price data",
    });
  });

  it("prefers JSON-LD over OpenGraph", () => {
    const html = `${fixture("jsonld-offer.html")}<meta property="og:price:amount" content="1"><meta property="og:price:currency" content="CAD">`;
    const result = extractPage(html, URL_BASE, new Map());
    assert.ok(result.kind === "product");
    assert.deepEqual([result.price, result.method], [149.99, "json-ld"]);
  });
});

describe("host extractor registry", () => {
  const fake: HostExtractor = () => ({
    kind: "product",
    price: 5,
    currency: "CAD",
    confidence: "high",
    method: "host",
    identifiers: {},
  });

  it("consults a registered host extractor first", () => {
    const registry = new Map([["shop.example.com", fake]]);
    const result = extractPage(fixture("jsonld-offer.html"), URL_BASE, registry);
    assert.equal(result.kind === "product" && result.method, "host");
  });

  it("falls through when the host extractor returns null", () => {
    const registry = new Map<string, HostExtractor>([["shop.example.com", () => null]]);
    assert.equal(extractPage(fixture("jsonld-offer.html"), URL_BASE, registry).kind, "product");
  });

  it("ignores extractors for other hosts and the default registry ships empty", () => {
    const registry = new Map([["other.example.com", fake]]);
    const result = extractPage(fixture("jsonld-offer.html"), URL_BASE, registry);
    assert.equal(result.kind === "product" && result.method, "json-ld");
    assert.equal(extractPage(fixture("jsonld-offer.html"), URL_BASE).kind, "product");
  });
});

describe("helpers", () => {
  it("parses price strings conservatively", () => {
    const cases: [unknown, number | null][] = [
      ["149", 149],
      ["$149", 149],
      ["1,299.99", 1299.99],
      ["1.299,99", 1299.99],
      ["12,50", 12.5],
      ["1,299", 1299],
      ["CAD 89.00", 89],
      [75, 75],
      ["$10 - $20", null],
      ["free", null],
      ["0", null],
      [-5, null],
      [null, null],
    ];
    for (const [input, expected] of cases) assert.equal(parsePrice(input), expected, String(input));
  });

  it("decodes common entities", () => {
    assert.equal(decodeEntities("A &amp; B &lt;3 &#65;&#x42; &unknown;"), "A & B <3 AB &unknown;");
  });

  it("normalizes urls", () => {
    assert.equal(
      normalizeUrl("/a?utm_medium=x&gclid=1&keep=2#frag", "https://h.example/b"),
      "https://h.example/a?keep=2",
    );
    assert.equal(normalizeUrl("mailto:x@y.z", "https://h.example/"), null);
  });
});

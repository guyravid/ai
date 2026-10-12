import type { JsonObject } from "../json.ts";

const STRING: JsonObject = { type: "string" };
const STRING_OR_NULL: JsonObject = { type: ["string", "null"] };
const STRING_LIST: JsonObject = { type: "array", items: STRING };
const SPEC_TYPES = ["general", "specific"];
const CONDITIONS = ["new", "used-ok", "used-only"];

function object(properties: Record<string, JsonObject>, required: string[]): JsonObject {
  return { type: "object", properties, required, additionalProperties: false };
}

function listOf(items: JsonObject, maxItems?: number): JsonObject {
  return maxItems === undefined ? { type: "array", items } : { type: "array", items, maxItems };
}

const MONEY: JsonObject = {
  type: ["object", "null"],
  properties: { amount: { type: "number" }, currency: STRING },
  required: ["amount", "currency"],
  additionalProperties: false,
};

const SPEC_SCHEMA = object(
  {
    type: { type: "string", enum: SPEC_TYPES },
    status: { type: "string", enum: ["draft", "ready"] },
    category: STRING,
    use: STRING,
    condition: { type: ["string", "null"], enum: [...CONDITIONS, null] },
    targetPrice: MONEY,
    maxPrice: MONEY,
    location: STRING,
    pickupRadiusKm: { type: ["number", "null"] },
    shippingOk: { type: ["boolean", "null"] },
    mustHave: STRING_LIST,
    niceToHave: STRING_LIST,
    dealBreakers: STRING_LIST,
    fit: STRING_LIST,
    unknownLines: STRING_LIST,
    unknownSections: STRING_LIST,
    productName: STRING,
    modelNumber: STRING,
    upc: STRING,
    asin: STRING,
    acceptableVariants: STRING_LIST,
  },
  ["type"],
);

const BRIEF_SCHEMA = object(
  {
    keywords: STRING_LIST,
    synonyms: STRING_LIST,
    exclude: STRING_LIST,
    updated: STRING,
    rules: STRING_LIST,
    unknownLines: STRING_LIST,
    unknownSections: STRING_LIST,
  },
  ["keywords", "synonyms", "exclude", "updated", "rules"],
);

const SEARCH_MATCH = object(
  {
    listingId: STRING,
    url: STRING,
    verdict: { type: "string", enum: ["match", "uncertain", "reject"] },
    price: { type: "number" },
    currency: STRING,
    reasons: listOf(STRING, 3),
    evidence: { type: "string", enum: ["page", "search"] },
    asOf: STRING,
  },
  ["verdict", "reasons", "evidence"],
);

const NEW_SOURCE = object(
  {
    name: STRING,
    url: STRING,
    kind: { type: "string", enum: ["product", "search"] },
    note: STRING,
  },
  ["name", "url", "kind", "note"],
);

function searcherSchema(mode: string): JsonObject {
  return object(
    {
      mode: { type: "string", enum: [mode] },
      matches: listOf(SEARCH_MATCH),
      newSources: listOf(NEW_SOURCE),
      notes: listOf({ type: "string", maxLength: 200 }, 3),
      specSuggestions: STRING_LIST,
    },
    ["mode", "matches", "newSources", "notes", "specSuggestions"],
  );
}

const PRICE_RANGE: JsonObject = {
  type: ["object", "null"],
  properties: { min: { type: "number" }, max: { type: "number" } },
  required: ["min", "max"],
  additionalProperties: false,
};

export const DEAL_SEARCHER_SCHEMAS = {
  "evaluate-listings": searcherSchema("evaluate-listings"),
  "check-url": searcherSchema("check-url"),
  sweep: searcherSchema("sweep"),
} as const satisfies Record<string, JsonObject>;

export const SPEC_REFINER_SCHEMAS = {
  build: object({ spec: SPEC_SCHEMA, questions: STRING_LIST, ready: { type: "boolean" } }, ["spec", "questions", "ready"]),
  "compile-brief": object({ brief: BRIEF_SCHEMA }, ["brief"]),
  "compact-notes": object({ brief: BRIEF_SCHEMA, keptNotes: listOf(STRING, 3) }, ["brief", "keptNotes"]),
} as const satisfies Record<string, JsonObject>;

export const PRODUCT_RESOLVER_SCHEMA: JsonObject = object(
  {
    type: { type: "string", enum: SPEC_TYPES },
    product: {
      type: ["object", "null"],
      properties: {
        name: STRING,
        manufacturer: STRING,
        modelNumber: STRING,
        upc: STRING,
        asin: STRING,
        variants: STRING_LIST,
        generationNote: STRING,
      },
      required: ["name", "modelNumber"],
      additionalProperties: false,
    },
    candidates: listOf(
      object(
        {
          name: STRING,
          why: STRING,
          newPrice: PRICE_RANGE,
          usedPrice: PRICE_RANGE,
          currency: STRING,
        },
        ["name", "why", "currency"],
      ),
    ),
    confidence: { type: "string", enum: ["high", "medium", "low"] },
    questions: STRING_LIST,
    sources: STRING_LIST,
  },
  ["type", "confidence", "questions", "sources"],
);

export type SearcherMode = keyof typeof DEAL_SEARCHER_SCHEMAS;

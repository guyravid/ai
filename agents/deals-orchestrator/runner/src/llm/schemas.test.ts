import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { isObject } from "../json.ts";
import {
  DEAL_SEARCHER_SCHEMAS,
  PRODUCT_RESOLVER_SCHEMA,
  SPEC_REFINER_SCHEMAS,
} from "./schemas.ts";

function collectObjectSchemas(node: unknown, found: Record<string, unknown>[] = []): Record<string, unknown>[] {
  if (Array.isArray(node)) {
    for (const entry of node) collectObjectSchemas(entry, found);
  } else if (isObject(node)) {
    if (node["type"] === "object" || (Array.isArray(node["type"]) && node["type"].includes("object"))) found.push(node);
    for (const value of Object.values(node)) collectObjectSchemas(value, found);
  }
  return found;
}

const ALL = {
  ...Object.fromEntries(Object.entries(DEAL_SEARCHER_SCHEMAS).map(([key, value]) => [`searcher:${key}`, value])),
  ...Object.fromEntries(Object.entries(SPEC_REFINER_SCHEMAS).map(([key, value]) => [`refiner:${key}`, value])),
  resolver: PRODUCT_RESOLVER_SCHEMA,
};

describe("schemas", () => {
  for (const [name, schema] of Object.entries(ALL)) {
    it(`${name}: every object forbids additional properties`, () => {
      const objects = collectObjectSchemas(schema).filter((node) => "properties" in node);
      assert.ok(objects.length > 0);
      for (const node of objects) assert.equal(node["additionalProperties"], false);
    });
  }

  it("constrains deal-searcher verdicts, evidence and reasons", () => {
    const schema = DEAL_SEARCHER_SCHEMAS["evaluate-listings"] as Record<string, unknown>;
    const text = JSON.stringify(schema);
    assert.ok(text.includes('"enum":["match","uncertain","reject"]'));
    assert.ok(text.includes('"enum":["page","search"]'));
    assert.ok(text.includes('"maxItems":3'));
  });

  it("pins each searcher schema to its mode", () => {
    for (const [mode, schema] of Object.entries(DEAL_SEARCHER_SCHEMAS)) {
      const properties = (schema as { properties: { mode: { enum: string[] } } }).properties;
      assert.deepEqual(properties.mode.enum, [mode]);
    }
  });

  it("caps keptNotes at 3", () => {
    assert.ok(JSON.stringify(SPEC_REFINER_SCHEMAS["compact-notes"]).includes('"maxItems":3'));
  });
});

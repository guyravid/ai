import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  emptyGeneralSpec,
  emptySearchBrief,
  emptySpecificSpec,
  isSearchReady,
  validateSpec,
  type GeneralSpec,
  type SpecificSpec,
} from "./spec.ts";
import { emptyLayout, parseDescription, serializeDescription } from "./template.ts";

function readyGeneral(): GeneralSpec {
  return {
    ...emptyGeneralSpec(),
    status: "ready",
    category: "Bikes",
    use: "Winter commuting and trails",
    condition: "used-ok",
    targetPrice: { amount: 150, currency: "CAD" },
    maxPrice: { amount: 200, currency: "CAD" },
    location: "Ottawa, ON",
    pickupRadiusKm: 50,
    shippingOk: false,
    mustHave: ["Disc brakes"],
    niceToHave: ["Rack mounts"],
    dealBreakers: ["Cracked frame"],
    fit: ["Rider height: 1.81 m"],
  };
}

function readySpecific(): SpecificSpec {
  return {
    ...emptySpecificSpec(),
    status: "ready",
    condition: "new",
    targetPrice: { amount: 449.99, currency: "USD" },
    maxPrice: { amount: 500, currency: "USD" },
    productName: "Air Pods Max 2",
    modelNumber: "A3454",
    upc: "195949000000",
    acceptableVariants: ["Any colour"],
  };
}

function wrap(spec: GeneralSpec | SpecificSpec) {
  return { spec, brief: null, notes: "", layout: emptyLayout() };
}

describe("round trip", () => {
  it("round-trips a general spec", () => {
    const spec = readyGeneral();
    assert.deepEqual(parseDescription(serializeDescription(wrap(spec))).spec, spec);
  });

  it("round-trips a specific spec", () => {
    const spec = readySpecific();
    assert.deepEqual(parseDescription(serializeDescription(wrap(spec))).spec, spec);
  });

  it("round-trips an empty draft spec", () => {
    for (const spec of [emptyGeneralSpec(), emptySpecificSpec()]) {
      assert.deepEqual(parseDescription(serializeDescription(wrap(spec))).spec, spec);
    }
  });

  it("round-trips a brief and notes", () => {
    const brief = {
      ...emptySearchBrief(),
      keywords: ["hardtail", "mtb"],
      synonyms: ["mountain bike"],
      exclude: ["kids"],
      updated: "2026-10-05",
      rules: ["Frame M or L", "No rim brakes"],
    };
    const parsed = parseDescription(
      serializeDescription({ spec: readyGeneral(), brief, notes: "line one\n\nline two", layout: emptyLayout() }),
    );
    assert.deepEqual(parsed.brief, brief);
    assert.equal(parsed.notes, "line one\n\nline two");
  });

  it("round-trips preserved unknown content held in the model", () => {
    const spec = {
      ...readyGeneral(),
      unknownLines: ["- **Colour:** green", "stray text"],
      unknownSections: ["### Seller notes\n- prefers cash"],
    };
    assert.deepEqual(parseDescription(serializeDescription(wrap(spec))).spec, spec);
  });

  it("serializes the canonical shape", () => {
    const text = serializeDescription(wrap(readyGeneral()));
    assert.match(text, /^## Deal Spec\n- \*\*Type:\*\* general\n- \*\*Status:\*\* ready\n/);
    assert.match(text, /- \*\*Target price:\*\* 150 CAD\n/);
    assert.match(text, /### Must have\n- Disc brakes/);
    assert.match(text, /\n\n## Notes$/);
    assert.ok(!text.includes("Product name"));
  });
});

describe("tolerant parsing", () => {
  const messy = [
    "Remember to check Kijiji first!",
    "",
    "##   deal spec  ",
    "* **TYPE** : General",
    "- **status:**ready",
    "-   **Target Price**:   1,250.50 cad",
    "- **max price:** CAD 1500",
    "- **Condition:** Used-OK",
    "- **Shipping OK:** NO",
    "- **Pickup radius km:** 40 km",
    "- **Category:** Bikes",
    "- **Use:** trails",
    "- **Colour preference:** green",
    "- **Condition:** mint",
    "random stray line",
    "",
    "### must HAVE",
    "- Disc brakes",
    "* Tubeless ready",
    "plain line without bullet",
    "### Seller red flags",
    "- no photos",
    "",
    "## Garage",
    "Stored at the cottage",
    "",
    "## NOTES",
    "My own thoughts",
    "",
    "second paragraph",
  ].join("\n");

  it("parses labels regardless of case and spacing", () => {
    const { spec } = parseDescription(messy);
    assert.ok(spec !== null && spec.type === "general");
    assert.equal(spec.status, "ready");
    assert.deepEqual(spec.targetPrice, { amount: 1250.5, currency: "CAD" });
    assert.deepEqual(spec.maxPrice, { amount: 1500, currency: "CAD" });
    assert.equal(spec.condition, "used-ok");
    assert.equal(spec.shippingOk, false);
    assert.equal(spec.pickupRadiusKm, 40);
    assert.deepEqual(spec.mustHave, ["Disc brakes", "Tubeless ready", "plain line without bullet"]);
  });

  it("keeps unrecognised and rejected content", () => {
    const { spec } = parseDescription(messy);
    assert.ok(spec !== null);
    assert.deepEqual(spec.unknownLines, ["- **Colour preference:** green", "- **Condition:** mint", "random stray line"]);
    assert.deepEqual(spec.unknownSections, ["### Seller red flags\n- no photos"]);
  });

  it("preserves unknown content and notes on serialize", () => {
    const output = serializeDescription(parseDescription(messy));
    for (const fragment of [
      "Remember to check Kijiji first!",
      "- **Colour preference:** green",
      "- **Condition:** mint",
      "random stray line",
      "### Seller red flags\n- no photos",
      "## Garage\nStored at the cottage",
      "## Notes\nMy own thoughts\n\nsecond paragraph",
    ]) {
      assert.ok(output.includes(fragment), `missing: ${fragment}`);
    }
    assert.ok(output.startsWith("Remember to check Kijiji first!\n\n## Deal Spec"));
    assert.ok(output.indexOf("## Garage") < output.indexOf("## Notes"));
  });

  it("is stable after one normalisation pass", () => {
    const once = serializeDescription(parseDescription(messy));
    assert.equal(serializeDescription(parseDescription(once)), once);
  });

  it("infers specific when product identity is present without a type", () => {
    const { spec } = parseDescription("## Deal Spec\n- **Model number:** X100\n- **Product name:** Camera");
    assert.ok(spec !== null && spec.type === "specific");
    assert.equal(spec.modelNumber, "X100");
  });

  it("treats specific-only fields on a general spec as unknown lines", () => {
    const { spec } = parseDescription("## Deal Spec\n- **Type:** general\n- **ASIN:** B000");
    assert.ok(spec !== null);
    assert.deepEqual(spec.unknownLines, ["- **ASIN:** B000"]);
  });

  it("defaults an invalid status to draft", () => {
    const { spec } = parseDescription("## Deal Spec\n- **Status:** done");
    assert.ok(spec !== null);
    assert.equal(spec.status, "draft");
    assert.deepEqual(spec.unknownLines, ["- **Status:** done"]);
  });
});

describe("bare descriptions", () => {
  it("parses empty and whitespace descriptions to no spec", () => {
    for (const text of ["", "   \n\n"]) {
      const parsed = parseDescription(text);
      assert.equal(parsed.spec, null);
      assert.equal(parsed.brief, null);
      assert.equal(serializeDescription(parsed), "");
    }
  });

  it("keeps plain text that has no sections", () => {
    const parsed = parseDescription("Just buy a bike\nsomeday");
    assert.equal(parsed.spec, null);
    assert.equal(serializeDescription(parsed), "Just buy a bike\nsomeday");
  });

  it("inserts a spec ahead of existing unknown sections", () => {
    const parsed = parseDescription("## Garage\nold stuff");
    parsed.spec = readyGeneral();
    const output = serializeDescription(parsed);
    assert.ok(output.indexOf("## Deal Spec") < output.indexOf("## Garage"));
    assert.ok(output.includes("## Garage\nold stuff"));
  });
});

describe("validateSpec", () => {
  it("accepts a complete general spec", () => {
    assert.deepEqual(validateSpec(readyGeneral()), []);
    assert.equal(isSearchReady(readyGeneral()), true);
  });

  it("accepts a complete specific spec", () => {
    assert.deepEqual(validateSpec(readySpecific()), []);
    assert.equal(isSearchReady(readySpecific()), true);
  });

  it("lists every missing general field", () => {
    assert.deepEqual(validateSpec(emptyGeneralSpec()), [
      "category",
      "use",
      "condition",
      "target price",
      "max price",
      "pickup radius or shipping ok = yes",
      "at least one must-have",
    ]);
  });

  it("lists every missing specific field", () => {
    assert.deepEqual(validateSpec(emptySpecificSpec()), [
      "product name",
      "model number",
      "condition",
      "target price",
      "max price",
    ]);
  });

  it("accepts shipping instead of a pickup radius", () => {
    const spec = { ...readyGeneral(), pickupRadiusKm: null, shippingOk: true };
    assert.deepEqual(validateSpec(spec), []);
  });

  it("rejects max below target and mixed currencies", () => {
    const below = { ...readyGeneral(), maxPrice: { amount: 100, currency: "CAD" } };
    const mixed = { ...readyGeneral(), maxPrice: { amount: 300, currency: "USD" } };
    assert.equal(validateSpec(below).length, 1);
    assert.equal(validateSpec(mixed).length, 1);
  });

  it("is not search-ready while in draft", () => {
    assert.equal(isSearchReady({ ...readyGeneral(), status: "draft" }), false);
    assert.equal(isSearchReady(null), false);
  });

  it("is not search-ready when ready but incomplete", () => {
    assert.equal(isSearchReady({ ...emptySpecificSpec(), status: "ready" }), false);
  });
});

import {
  ValidationError,
  expectBoolean,
  expectNumber,
  expectObject,
  expectOneOf,
  expectString,
  expectStringArray,
  isNullish,
  type JsonObject,
} from "./json.ts";
import {
  CONDITIONS,
  SPEC_STATUSES,
  SPEC_TYPES,
  emptyGeneralSpec,
  emptySearchBrief,
  emptySpecificSpec,
  type Money,
  type SearchBrief,
  type Spec,
} from "./spec.ts";

const COMMON_KEYS = [
  "type",
  "status",
  "category",
  "use",
  "condition",
  "targetPrice",
  "maxPrice",
  "location",
  "pickupRadiusKm",
  "shippingOk",
  "mustHave",
  "niceToHave",
  "dealBreakers",
  "fit",
  "unknownLines",
  "unknownSections",
];
const SPECIFIC_KEYS = ["productName", "modelNumber", "upc", "asin", "acceptableVariants"];
const BRIEF_KEYS = ["keywords", "synonyms", "exclude", "updated", "rules", "unknownLines", "unknownSections"];

function rejectUnknownKeys(object: JsonObject, allowed: readonly string[], path: string): void {
  for (const key of Object.keys(object)) {
    if (!allowed.includes(key)) throw new ValidationError(`${path}.${key} is not a known field`);
  }
}

function singleLine(value: string, path: string): string {
  if (/[\r\n]/.test(value)) throw new ValidationError(`${path} must be a single line`);
  return value.trim();
}

function textField(object: JsonObject, key: string, path: string, fallback: string): string {
  const value = object[key];
  return isNullish(value) ? fallback : singleLine(expectString(value, `${path}.${key}`), `${path}.${key}`);
}

function listField(object: JsonObject, key: string, path: string): string[] {
  const value = object[key];
  if (isNullish(value)) return [];
  return expectStringArray(value, `${path}.${key}`).map((entry, index) => singleLine(entry, `${path}.${key}[${index}]`));
}

function rawBlockList(object: JsonObject, key: string, path: string): string[] {
  const value = object[key];
  return isNullish(value) ? [] : expectStringArray(value, `${path}.${key}`);
}

function moneyField(object: JsonObject, key: string, path: string): Money | null {
  const value = object[key];
  if (isNullish(value)) return null;
  const money = expectObject(value, `${path}.${key}`);
  rejectUnknownKeys(money, ["amount", "currency"], `${path}.${key}`);
  const amount = expectNumber(money["amount"], `${path}.${key}.amount`);
  const currency = singleLine(expectString(money["currency"], `${path}.${key}.currency`), `${path}.${key}.currency`);
  return { amount, currency: currency.toUpperCase() };
}

function optionalEnum<T extends string>(object: JsonObject, key: string, allowed: readonly T[], path: string): T | null {
  const value = object[key];
  return isNullish(value) ? null : expectOneOf(value, allowed, `${path}.${key}`);
}

function optionalNumber(object: JsonObject, key: string, path: string): number | null {
  const value = object[key];
  return isNullish(value) ? null : expectNumber(value, `${path}.${key}`);
}

function optionalBoolean(object: JsonObject, key: string, path: string): boolean | null {
  const value = object[key];
  return isNullish(value) ? null : expectBoolean(value, `${path}.${key}`);
}

export function parseSpecJson(raw: unknown, path = "spec"): Spec {
  const object = expectObject(raw, path);
  const type = expectOneOf(object["type"], SPEC_TYPES, `${path}.type`);
  rejectUnknownKeys(object, type === "specific" ? [...COMMON_KEYS, ...SPECIFIC_KEYS] : COMMON_KEYS, path);

  const spec: Spec = type === "specific" ? emptySpecificSpec() : emptyGeneralSpec();
  spec.status = optionalEnum(object, "status", SPEC_STATUSES, path) ?? "draft";
  spec.category = textField(object, "category", path, "");
  spec.use = textField(object, "use", path, "");
  spec.condition = optionalEnum(object, "condition", CONDITIONS, path);
  spec.targetPrice = moneyField(object, "targetPrice", path);
  spec.maxPrice = moneyField(object, "maxPrice", path);
  spec.location = textField(object, "location", path, "");
  spec.pickupRadiusKm = optionalNumber(object, "pickupRadiusKm", path);
  spec.shippingOk = optionalBoolean(object, "shippingOk", path);
  spec.mustHave = listField(object, "mustHave", path);
  spec.niceToHave = listField(object, "niceToHave", path);
  spec.dealBreakers = listField(object, "dealBreakers", path);
  spec.fit = listField(object, "fit", path);
  spec.unknownLines = rawBlockList(object, "unknownLines", path);
  spec.unknownSections = rawBlockList(object, "unknownSections", path);

  if (spec.type === "specific") {
    spec.productName = textField(object, "productName", path, "");
    spec.modelNumber = textField(object, "modelNumber", path, "");
    spec.upc = textField(object, "upc", path, "");
    spec.asin = textField(object, "asin", path, "");
    spec.acceptableVariants = listField(object, "acceptableVariants", path);
  }
  return spec;
}

export function parseBriefJson(raw: unknown, path = "brief"): SearchBrief {
  const object = expectObject(raw, path);
  rejectUnknownKeys(object, BRIEF_KEYS, path);
  const brief = emptySearchBrief();
  brief.keywords = listField(object, "keywords", path);
  brief.synonyms = listField(object, "synonyms", path);
  brief.exclude = listField(object, "exclude", path);
  brief.updated = textField(object, "updated", path, "");
  brief.rules = listField(object, "rules", path);
  brief.unknownLines = rawBlockList(object, "unknownLines", path);
  brief.unknownSections = rawBlockList(object, "unknownSections", path);
  return brief;
}

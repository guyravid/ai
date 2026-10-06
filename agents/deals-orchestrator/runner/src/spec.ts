export const CONDITIONS = ["new", "used-ok", "used-only"] as const;
export type Condition = (typeof CONDITIONS)[number];

export const SPEC_STATUSES = ["draft", "ready"] as const;
export type SpecStatus = (typeof SPEC_STATUSES)[number];

export const SPEC_TYPES = ["general", "specific"] as const;
export type SpecType = (typeof SPEC_TYPES)[number];

export interface Money {
  amount: number;
  currency: string;
}

interface SpecCommon {
  status: SpecStatus;
  category: string;
  use: string;
  condition: Condition | null;
  targetPrice: Money | null;
  maxPrice: Money | null;
  location: string;
  pickupRadiusKm: number | null;
  shippingOk: boolean | null;
  mustHave: string[];
  niceToHave: string[];
  dealBreakers: string[];
  fit: string[];
  unknownLines: string[];
  unknownSections: string[];
}

export interface GeneralSpec extends SpecCommon {
  type: "general";
}

export interface SpecificSpec extends SpecCommon {
  type: "specific";
  productName: string;
  modelNumber: string;
  upc: string;
  asin: string;
  acceptableVariants: string[];
}

export type Spec = GeneralSpec | SpecificSpec;

export interface SearchBrief {
  keywords: string[];
  synonyms: string[];
  exclude: string[];
  updated: string;
  rules: string[];
  unknownLines: string[];
  unknownSections: string[];
}

export function emptyGeneralSpec(): GeneralSpec {
  return {
    type: "general",
    status: "draft",
    category: "",
    use: "",
    condition: null,
    targetPrice: null,
    maxPrice: null,
    location: "",
    pickupRadiusKm: null,
    shippingOk: null,
    mustHave: [],
    niceToHave: [],
    dealBreakers: [],
    fit: [],
    unknownLines: [],
    unknownSections: [],
  };
}

export function emptySpecificSpec(): SpecificSpec {
  const { type: _type, ...common } = emptyGeneralSpec();
  return {
    ...common,
    type: "specific",
    productName: "",
    modelNumber: "",
    upc: "",
    asin: "",
    acceptableVariants: [],
  };
}

export function emptySearchBrief(): SearchBrief {
  return {
    keywords: [],
    synonyms: [],
    exclude: [],
    updated: "",
    rules: [],
    unknownLines: [],
    unknownSections: [],
  };
}

function isValidMoney(price: Money | null): price is Money {
  return price !== null && Number.isFinite(price.amount) && price.amount > 0 && price.currency.trim() !== "";
}

function isBlank(text: string): boolean {
  return text.trim() === "";
}

function validateCommon(spec: Spec): string[] {
  const problems: string[] = [];
  if (spec.condition === null) problems.push("condition");
  if (!isValidMoney(spec.targetPrice)) problems.push("target price");
  if (!isValidMoney(spec.maxPrice)) problems.push("max price");
  if (
    isValidMoney(spec.targetPrice) &&
    isValidMoney(spec.maxPrice) &&
    (spec.targetPrice.currency !== spec.maxPrice.currency || spec.maxPrice.amount < spec.targetPrice.amount)
  ) {
    problems.push("max price must be in the same currency and not below target price");
  }
  return problems;
}

function validateGeneral(spec: Spec): string[] {
  const problems: string[] = [];
  if (isBlank(spec.category)) problems.push("category");
  if (isBlank(spec.use)) problems.push("use");
  problems.push(...validateCommon(spec));
  const hasPickup = spec.pickupRadiusKm !== null && spec.pickupRadiusKm > 0;
  if (!hasPickup && spec.shippingOk !== true) {
    problems.push("pickup radius or shipping ok = yes");
  }
  if (!spec.mustHave.some((entry) => !isBlank(entry))) problems.push("at least one must-have");
  return problems;
}

function validateSpecific(spec: SpecificSpec): string[] {
  const problems: string[] = [];
  if (isBlank(spec.productName)) problems.push("product name");
  if (isBlank(spec.modelNumber)) problems.push("model number");
  problems.push(...validateCommon(spec));
  return problems;
}

export function validateSpec(spec: Spec): string[] {
  return spec.type === "specific" ? validateSpecific(spec) : validateGeneral(spec);
}

export function isSearchReady(spec: Spec | null): boolean {
  return spec !== null && spec.status === "ready" && validateSpec(spec).length === 0;
}

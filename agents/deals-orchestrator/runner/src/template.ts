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

export type LayoutSection =
  | { kind: "spec" }
  | { kind: "brief" }
  | { kind: "notes" }
  | { kind: "raw"; text: string };

export interface CardLayout {
  preamble: string;
  sections: LayoutSection[];
}

export interface ParsedDescription {
  spec: Spec | null;
  brief: SearchBrief | null;
  notes: string;
  layout: CardLayout;
}

export function emptyLayout(): CardLayout {
  return { preamble: "", sections: [] };
}

type Parsed<T> = { ok: true; value: T } | { ok: false };

const H2 = /^##(?!#)[ \t]*(.*?)[ \t]*#*[ \t]*$/;
const H3 = /^###(?!#)[ \t]*(.*?)[ \t]*#*[ \t]*$/;
const FIELD = /^[ \t]*[-*+][ \t]*\*\*(.+?)\*\*[ \t]*(.*)$/;
const BULLET = /^[-*+][ \t]+(.*)$/;

function normalizeTitle(title: string): string {
  return title.toLowerCase().replace(/\s+/g, " ").trim();
}

function splitLines(text: string): string[] {
  return text.replace(/\r\n?/g, "\n").split("\n");
}

function trimBlankEdges(lines: string[]): string[] {
  let start = 0;
  let end = lines.length;
  while (start < end && (lines[start] ?? "").trim() === "") start += 1;
  while (end > start && (lines[end - 1] ?? "").trim() === "") end -= 1;
  return lines.slice(start, end);
}

function trimTrailingBlanks(lines: string[]): string[] {
  let end = lines.length;
  while (end > 0 && (lines[end - 1] ?? "").trim() === "") end -= 1;
  return lines.slice(0, end).map((line) => line.trimEnd());
}

interface RawBlock {
  title: string;
  heading: string;
  body: string[];
}

function splitByHeading(lines: string[], pattern: RegExp): { before: string[]; blocks: RawBlock[] } {
  const before: string[] = [];
  const blocks: RawBlock[] = [];
  for (const line of lines) {
    const match = pattern.exec(line);
    if (match !== null) {
      blocks.push({ title: normalizeTitle(match[1] ?? ""), heading: line, body: [] });
    } else if (blocks.length === 0) {
      before.push(line);
    } else {
      blocks[blocks.length - 1]?.body.push(line);
    }
  }
  return { before, blocks };
}

function rawBlockText(block: RawBlock): string {
  return trimTrailingBlanks([block.heading, ...block.body]).join("\n");
}

interface FieldEntry {
  raw: string;
  label: string | null;
  value: string;
}

function readFieldEntries(lines: string[]): FieldEntry[] {
  const entries: FieldEntry[] = [];
  for (const line of lines) {
    if (line.trim() === "") continue;
    const match = FIELD.exec(line);
    if (match === null) {
      entries.push({ raw: line.trimEnd(), label: null, value: "" });
      continue;
    }
    const label = normalizeTitle((match[1] ?? "").replace(/:\s*$/, ""));
    const value = (match[2] ?? "").replace(/^[ \t]*:[ \t]*/, "").trim();
    entries.push({ raw: line.trimEnd(), label, value });
  }
  return entries;
}

function readBullets(lines: string[]): string[] {
  const items: string[] = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === "") continue;
    const match = BULLET.exec(trimmed);
    const text = (match === null ? trimmed : (match[1] ?? "")).trim();
    if (text !== "" && !/^[-*+]$/.test(text)) items.push(text);
  }
  return items;
}

function parseEnum<T extends string>(value: string, allowed: readonly T[]): Parsed<T | null> {
  if (value === "") return { ok: true, value: null };
  const match = allowed.find((candidate) => candidate === value.toLowerCase());
  return match === undefined ? { ok: false } : { ok: true, value: match };
}

function parseMoney(value: string): Parsed<Money | null> {
  if (value === "") return { ok: true, value: null };
  const amountFirst = /^([0-9][0-9,]*(?:\.[0-9]+)?)\s*([A-Za-z]{3})$/.exec(value);
  const currencyFirst = /^([A-Za-z]{3})\s*([0-9][0-9,]*(?:\.[0-9]+)?)$/.exec(value);
  const amountText = amountFirst?.[1] ?? currencyFirst?.[2];
  const currency = amountFirst?.[2] ?? currencyFirst?.[1];
  if (amountText === undefined || currency === undefined) return { ok: false };
  return { ok: true, value: { amount: Number(amountText.replace(/,/g, "")), currency: currency.toUpperCase() } };
}

function parseRadius(value: string): Parsed<number | null> {
  if (value === "") return { ok: true, value: null };
  const match = /^(\d+(?:\.\d+)?)\s*(?:km)?$/i.exec(value);
  return match === null ? { ok: false } : { ok: true, value: Number(match[1]) };
}

function parseYesNo(value: string): Parsed<boolean | null> {
  const result = parseEnum(value, ["yes", "no"] as const);
  if (!result.ok) return result;
  return { ok: true, value: result.value === null ? null : result.value === "yes" };
}

function parseCommaList(value: string): string[] {
  return value
    .split(",")
    .map((entry) => entry.trim())
    .filter((entry) => entry !== "");
}

function inferSpecType(entries: FieldEntry[]): "general" | "specific" {
  const declared = entries.find((entry) => entry.label === "type");
  const parsed = parseEnum(declared?.value ?? "", SPEC_TYPES);
  if (parsed.ok && parsed.value !== null) return parsed.value;
  const hasProductIdentity = entries.some(
    (entry) => (entry.label === "product name" || entry.label === "model number") && entry.value !== "",
  );
  return hasProductIdentity ? "specific" : "general";
}

function applySpecField(spec: Spec, label: string, value: string): boolean {
  switch (label) {
    case "type":
      return value.toLowerCase() === spec.type;
    case "status": {
      const status = parseEnum(value, SPEC_STATUSES);
      if (!status.ok) return false;
      spec.status = status.value ?? "draft";
      return true;
    }
    case "category":
      spec.category = value;
      return true;
    case "use":
      spec.use = value;
      return true;
    case "condition": {
      const condition = parseEnum(value, CONDITIONS);
      if (!condition.ok) return false;
      spec.condition = condition.value;
      return true;
    }
    case "target price":
    case "max price": {
      const price = parseMoney(value);
      if (!price.ok) return false;
      if (label === "target price") spec.targetPrice = price.value;
      else spec.maxPrice = price.value;
      return true;
    }
    case "location":
      spec.location = value;
      return true;
    case "pickup radius km": {
      const radius = parseRadius(value);
      if (!radius.ok) return false;
      spec.pickupRadiusKm = radius.value;
      return true;
    }
    case "shipping ok": {
      const shipping = parseYesNo(value);
      if (!shipping.ok) return false;
      spec.shippingOk = shipping.value;
      return true;
    }
    default:
      return applySpecificField(spec, label, value);
  }
}

function applySpecificField(spec: Spec, label: string, value: string): boolean {
  if (spec.type !== "specific") return false;
  switch (label) {
    case "product name":
      spec.productName = value;
      return true;
    case "model number":
      spec.modelNumber = value;
      return true;
    case "upc/ean":
      spec.upc = value;
      return true;
    case "asin":
      spec.asin = value;
      return true;
    default:
      return false;
  }
}

function listFieldFor(spec: Spec, title: string): string[] | null {
  switch (title) {
    case "must have":
      return spec.mustHave;
    case "nice to have":
      return spec.niceToHave;
    case "deal breakers":
      return spec.dealBreakers;
    case "fit":
      return spec.fit;
    case "acceptable variants":
      return spec.type === "specific" ? spec.acceptableVariants : null;
    default:
      return null;
  }
}

function parseSpecBody(body: string[]): Spec {
  const { before, blocks } = splitByHeading(body, H3);
  const entries = readFieldEntries(before);
  const spec: Spec = inferSpecType(entries) === "specific" ? emptySpecificSpec() : emptyGeneralSpec();

  for (const entry of entries) {
    const accepted = entry.label !== null && applySpecField(spec, entry.label, entry.value);
    if (!accepted) spec.unknownLines.push(entry.raw);
  }

  const seenTitles = new Set<string>();
  for (const block of blocks) {
    const target = seenTitles.has(block.title) ? null : listFieldFor(spec, block.title);
    if (target === null) {
      spec.unknownSections.push(rawBlockText(block));
    } else {
      seenTitles.add(block.title);
      target.push(...readBullets(block.body));
    }
  }
  return spec;
}

function parseBriefBody(body: string[]): SearchBrief {
  const { before, blocks } = splitByHeading(body, H3);
  const brief = emptySearchBrief();

  for (const entry of readFieldEntries(before)) {
    switch (entry.label) {
      case "keywords":
        brief.keywords = parseCommaList(entry.value);
        break;
      case "synonyms":
        brief.synonyms = parseCommaList(entry.value);
        break;
      case "exclude":
        brief.exclude = parseCommaList(entry.value);
        break;
      case "updated":
        brief.updated = entry.value;
        break;
      default:
        brief.unknownLines.push(entry.raw);
    }
  }

  let rulesSeen = false;
  for (const block of blocks) {
    if (block.title === "rules" && !rulesSeen) {
      rulesSeen = true;
      brief.rules.push(...readBullets(block.body));
    } else {
      brief.unknownSections.push(rawBlockText(block));
    }
  }
  return brief;
}

export function parseDescription(description: string): ParsedDescription {
  const { before, blocks } = splitByHeading(splitLines(description), H2);
  const result: ParsedDescription = {
    spec: null,
    brief: null,
    notes: "",
    layout: { preamble: trimTrailingBlanks(trimBlankEdges(before)).join("\n"), sections: [] },
  };

  for (const block of blocks) {
    if (block.title === "deal spec" && result.spec === null) {
      result.spec = parseSpecBody(block.body);
      result.layout.sections.push({ kind: "spec" });
    } else if (block.title === "search brief" && result.brief === null) {
      result.brief = parseBriefBody(block.body);
      result.layout.sections.push({ kind: "brief" });
    } else if (block.title === "notes" && !result.layout.sections.some((section) => section.kind === "notes")) {
      result.notes = trimBlankEdges(block.body).join("\n");
      result.layout.sections.push({ kind: "notes" });
    } else {
      result.layout.sections.push({ kind: "raw", text: rawBlockText(block) });
    }
  }
  return result;
}

function fieldLine(label: string, value: string): string {
  return value === "" ? `- **${label}:**` : `- **${label}:** ${value}`;
}

function formatMoney(price: Money | null): string {
  return price === null ? "" : `${price.amount} ${price.currency}`;
}

function bulletBlock(title: string, items: string[]): string[] {
  return ["", `### ${title}`, ...items.map((item) => `- ${item}`)];
}

function serializeSpecSection(spec: Spec): string {
  const lines = [
    "## Deal Spec",
    fieldLine("Type", spec.type),
    fieldLine("Status", spec.status),
    fieldLine("Category", spec.category),
    fieldLine("Use", spec.use),
    fieldLine("Condition", spec.condition ?? ""),
    fieldLine("Target price", formatMoney(spec.targetPrice)),
    fieldLine("Max price", formatMoney(spec.maxPrice)),
    fieldLine("Location", spec.location),
    fieldLine("Pickup radius km", spec.pickupRadiusKm === null ? "" : String(spec.pickupRadiusKm)),
    fieldLine("Shipping OK", spec.shippingOk === null ? "" : spec.shippingOk ? "yes" : "no"),
  ];
  if (spec.type === "specific") {
    lines.push(
      fieldLine("Product name", spec.productName),
      fieldLine("Model number", spec.modelNumber),
      fieldLine("UPC/EAN", spec.upc),
      fieldLine("ASIN", spec.asin),
    );
  }
  lines.push(...spec.unknownLines);
  lines.push(...bulletBlock("Must have", spec.mustHave));
  lines.push(...bulletBlock("Nice to have", spec.niceToHave));
  lines.push(...bulletBlock("Deal breakers", spec.dealBreakers));
  lines.push(...bulletBlock("Fit", spec.fit));
  if (spec.type === "specific") lines.push(...bulletBlock("Acceptable variants", spec.acceptableVariants));
  for (const section of spec.unknownSections) lines.push("", section);
  return lines.join("\n");
}

function serializeBriefSection(brief: SearchBrief): string {
  const lines = [
    "## Search brief",
    fieldLine("Keywords", brief.keywords.join(", ")),
    fieldLine("Synonyms", brief.synonyms.join(", ")),
    fieldLine("Exclude", brief.exclude.join(", ")),
    fieldLine("Updated", brief.updated),
    ...brief.unknownLines,
    ...bulletBlock("Rules", brief.rules),
  ];
  for (const section of brief.unknownSections) lines.push("", section);
  return lines.join("\n");
}

function serializeNotesSection(notes: string): string {
  return notes === "" ? "## Notes" : `## Notes\n${notes}`;
}

function renderSection(section: LayoutSection, document: ParsedDescription): string | null {
  switch (section.kind) {
    case "spec":
      return document.spec === null ? null : serializeSpecSection(document.spec);
    case "brief":
      return document.brief === null ? null : serializeBriefSection(document.brief);
    case "notes":
      return serializeNotesSection(document.notes);
    case "raw":
      return section.text;
  }
}

function withMissingKnownSections(document: ParsedDescription): LayoutSection[] {
  const existing = document.layout.sections;
  const has = (kind: LayoutSection["kind"]): boolean => existing.some((section) => section.kind === kind);
  const front: LayoutSection[] = [];
  if (document.spec !== null && !has("spec")) front.push({ kind: "spec" });
  if (document.brief !== null && !has("brief")) front.push({ kind: "brief" });
  const needsNotes = !has("notes") && (document.notes !== "" || document.spec !== null || document.brief !== null);
  return [...front, ...existing, ...(needsNotes ? [{ kind: "notes" } as const] : [])];
}

export function serializeDescription(document: ParsedDescription): string {
  const rendered = withMissingKnownSections(document).map((section) => renderSection(section, document));
  const parts = [document.layout.preamble, ...rendered].filter(
    (part): part is string => part !== null && part !== "",
  );
  return parts.join("\n\n");
}

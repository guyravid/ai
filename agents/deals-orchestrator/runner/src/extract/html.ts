const NAMED_ENTITIES: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  nbsp: " ",
};

const TRACKING_PARAMS = ["fbclid", "gclid"];

export function decodeEntities(text: string): string {
  return text.replace(/&(#x[0-9a-f]+|#[0-9]+|[a-z]+);/gi, (match, entity: string) => {
    if (entity.startsWith("#x") || entity.startsWith("#X")) return codePointOrOriginal(parseInt(entity.slice(2), 16), match);
    if (entity.startsWith("#")) return codePointOrOriginal(parseInt(entity.slice(1), 10), match);
    return NAMED_ENTITIES[entity.toLowerCase()] ?? match;
  });
}

function codePointOrOriginal(codePoint: number, original: string): string {
  return Number.isInteger(codePoint) && codePoint > 0 && codePoint <= 0x10ffff
    ? String.fromCodePoint(codePoint)
    : original;
}

export function parseAttributes(tag: string): Map<string, string> {
  const attributes = new Map<string, string>();
  const pattern = /([^\s=/>"']+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g;
  const withoutName = tag.replace(/^<\s*[^\s>/]+/, "");
  for (const match of withoutName.matchAll(pattern)) {
    const name = (match[1] ?? "").toLowerCase();
    if (!attributes.has(name)) attributes.set(name, decodeEntities(match[2] ?? match[3] ?? match[4] ?? ""));
  }
  return attributes;
}

export interface FoundTag {
  raw: string;
  end: number;
  attributes: Map<string, string>;
}

export function findTags(html: string, tagName: string): FoundTag[] {
  const pattern = new RegExp(`<${tagName}\\b[^>]*>`, "gi");
  return [...html.matchAll(pattern)].map((match) => ({
    raw: match[0],
    end: (match.index ?? 0) + match[0].length,
    attributes: parseAttributes(match[0]),
  }));
}

export function parsePrice(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) && value > 0 ? value : null;
  if (typeof value !== "string") return null;
  const groups = value.match(/[0-9][0-9.,]*/g);
  if (groups === null || groups.length !== 1) return null;
  const amount = Number(normalizeNumberText(groups[0] ?? "").replace(/[^0-9.]/g, ""));
  return Number.isFinite(amount) && amount > 0 ? amount : null;
}

function normalizeNumberText(text: string): string {
  const trimmed = text.replace(/[.,]+$/, "");
  const lastComma = trimmed.lastIndexOf(",");
  const lastDot = trimmed.lastIndexOf(".");
  if (lastComma >= 0 && lastDot >= 0) {
    const decimalSeparator = lastComma > lastDot ? "," : ".";
    const thousandsSeparator = decimalSeparator === "," ? "." : ",";
    return trimmed.split(thousandsSeparator).join("").replace(decimalSeparator, ".");
  }
  if (lastComma >= 0) {
    const commaCount = trimmed.split(",").length - 1;
    const digitsAfter = trimmed.length - lastComma - 1;
    return commaCount === 1 && digitsAfter !== 3 ? trimmed.replace(",", ".") : trimmed.split(",").join("");
  }
  return trimmed;
}

export function normalizeUrl(href: string, base: string): string | null {
  let url: URL;
  try {
    url = new URL(decodeEntities(href.trim()), base);
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  url.hash = "";
  for (const key of [...url.searchParams.keys()]) {
    if (key.toLowerCase().startsWith("utm_") || TRACKING_PARAMS.includes(key.toLowerCase())) {
      url.searchParams.delete(key);
    }
  }
  return url.toString();
}

import type { Source } from "./item-store.ts";
import type { AttachmentRecord } from "./trello-records.ts";

const SOURCE_NAME = /^(best source|source)[ \t]*-[ \t]*(.+)$/i;

export function parseSourceAttachment(attachment: AttachmentRecord): Source | null {
  const match = SOURCE_NAME.exec(attachment.name.trim());
  const name = match?.[2]?.trim();
  if (match === null || name === undefined || name === "" || attachment.url === "") return null;
  return {
    id: attachment.id,
    name,
    url: attachment.url,
    best: (match[1] ?? "").toLowerCase() === "best source",
  };
}

export function formatSourceName(name: string, best: boolean): string {
  return best ? `Best Source - ${name}` : `Source - ${name}`;
}

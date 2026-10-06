import { isObject } from "../json.ts";
import { StoreError } from "./store-error.ts";

export interface ExecResult {
  stdout: string;
  exitCode: number;
}

export type Exec = (args: string[]) => Promise<ExecResult>;

interface Page {
  hasMore: boolean;
  nextCursor: string | null;
}

interface Envelope {
  data: unknown;
  page: Page;
}

function parseEnvelopeText(result: ExecResult): unknown {
  try {
    return JSON.parse(result.stdout);
  } catch {
    throw new StoreError("internal", `trello exited ${result.exitCode} without a JSON envelope`);
  }
}

function toStoreError(error: unknown): StoreError {
  if (!isObject(error)) return new StoreError("internal", "trello returned a malformed error");
  const code = typeof error["code"] === "string" ? error["code"] : "internal";
  const message = typeof error["message"] === "string" ? error["message"] : "trello reported an error";
  return new StoreError(code, message, error["retriable"] === true);
}

function parsePage(meta: unknown): Page {
  const page = isObject(meta) ? meta["page"] : undefined;
  if (!isObject(page)) return { hasMore: false, nextCursor: null };
  const nextCursor = typeof page["next_cursor"] === "string" ? page["next_cursor"] : null;
  return { hasMore: page["has_more"] === true && nextCursor !== null, nextCursor };
}

export function parseEnvelope(result: ExecResult): Envelope {
  const root = parseEnvelopeText(result);
  if (!isObject(root) || typeof root["ok"] !== "boolean") {
    throw new StoreError("internal", "trello returned an unrecognised envelope");
  }
  if (!root["ok"]) throw toStoreError(root["error"]);
  return { data: root["data"] ?? null, page: parsePage(root["meta"]) };
}

export class TrelloClient {
  private readonly exec: Exec;

  constructor(exec: Exec) {
    this.exec = exec;
  }

  async read(args: string[]): Promise<Envelope> {
    return parseEnvelope(await this.exec(args));
  }

  async write(args: string[]): Promise<unknown> {
    return parseEnvelope(await this.exec([...args, "--confirm"])).data;
  }

  async readAll(requiredArgs: string[], tuningArgs: string[]): Promise<unknown[]> {
    const records: unknown[] = [];
    let envelope = await this.read([...requiredArgs, ...tuningArgs]);
    for (;;) {
      if (envelope.data !== null) {
        if (!Array.isArray(envelope.data)) throw new StoreError("internal", "trello list result is not an array");
        records.push(...envelope.data);
      }
      if (!envelope.page.hasMore || envelope.page.nextCursor === null) return records;
      envelope = await this.read([...requiredArgs, "--cursor", envelope.page.nextCursor]);
    }
  }
}

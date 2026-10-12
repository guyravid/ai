import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { join } from "node:path";
import { ValidationError, expectNumber, expectObject, isNullish } from "../json.ts";

export interface Ledger {
  spentOn(dateKey: string): Promise<number>;
  record(dateKey: string, costUsd: number): Promise<void>;
  hasNotice(dateKey: string, key: string): Promise<boolean>;
  markNotice(dateKey: string, key: string, at: string): Promise<void>;
}

interface LedgerDay {
  date: string;
  costUsd: number;
  notices: Record<string, string>;
}

export function localDateKey(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

function parseDay(raw: unknown, dateKey: string): LedgerDay {
  const object = expectObject(raw, "ledger");
  const rawNotices = isNullish(object["notices"]) ? {} : expectObject(object["notices"], "ledger.notices");
  const notices: Record<string, string> = {};
  for (const [key, value] of Object.entries(rawNotices)) notices[key] = String(value);
  return { date: dateKey, costUsd: expectNumber(object["costUsd"], "ledger.costUsd"), notices };
}

export class FileLedger implements Ledger {
  private readonly directory: string;

  constructor(directory: string) {
    this.directory = directory;
  }

  async spentOn(dateKey: string): Promise<number> {
    return (await this.read(dateKey)).costUsd;
  }

  async record(dateKey: string, costUsd: number): Promise<void> {
    const day = await this.read(dateKey);
    await this.write({ ...day, costUsd: day.costUsd + costUsd });
  }

  async hasNotice(dateKey: string, key: string): Promise<boolean> {
    return key in (await this.read(dateKey)).notices;
  }

  async markNotice(dateKey: string, key: string, at: string): Promise<void> {
    const day = await this.read(dateKey);
    await this.write({ ...day, notices: { ...day.notices, [key]: at } });
  }

  private async read(dateKey: string): Promise<LedgerDay> {
    try {
      return parseDay(JSON.parse(await readFile(this.pathFor(dateKey), "utf8")), dateKey);
    } catch (error) {
      if (typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT") {
        return { date: dateKey, costUsd: 0, notices: {} };
      }
      if (error instanceof SyntaxError || error instanceof ValidationError) {
        throw new Error(`corrupt ledger for ${dateKey}: ${error.message}`);
      }
      throw error;
    }
  }

  private async write(day: LedgerDay): Promise<void> {
    const path = this.pathFor(day.date);
    const temporaryPath = `${path}.${randomBytes(6).toString("hex")}.tmp`;
    await mkdir(this.directory, { recursive: true });
    await writeFile(temporaryPath, `${JSON.stringify(day)}\n`, "utf8");
    await rename(temporaryPath, path);
  }

  private pathFor(dateKey: string): string {
    return join(this.directory, `ledger-${dateKey}.json`);
  }
}

import { appendFile, mkdir } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

export interface TelemetrySink {
  append(line: Record<string, unknown>): Promise<void>;
}

export function resolveLogDir(env: NodeJS.ProcessEnv): string {
  const override = env["DEALS_LOG_DIR"];
  if (override !== undefined && override !== "") return override;
  return join(homedir(), "Library", "Logs", "deals");
}

export class FileTelemetry implements TelemetrySink {
  private readonly directory: string;

  constructor(directory: string) {
    this.directory = directory;
  }

  async append(line: Record<string, unknown>): Promise<void> {
    await mkdir(this.directory, { recursive: true });
    await appendFile(join(this.directory, "runs.jsonl"), `${JSON.stringify(line)}\n`, "utf8");
  }
}

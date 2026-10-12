import { randomBytes } from "node:crypto";
import { rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { isObject } from "../json.ts";
import type { ExecResult } from "../llm/claude-invocation.ts";

export type SendResult = { ok: true } | { ok: false; error: string };

export interface AlertSender {
  send(text: string): Promise<SendResult>;
}

export type TelegramExec = (args: string[]) => Promise<ExecResult>;

export class TelegramSender implements AlertSender {
  private readonly exec: TelegramExec;
  private readonly directory: string;
  private readonly chat: string | null;

  constructor(options: { exec: TelegramExec; directory: string; chat: string | null }) {
    this.exec = options.exec;
    this.directory = options.directory;
    this.chat = options.chat;
  }

  async send(text: string): Promise<SendResult> {
    const path = join(this.directory, `alert-${randomBytes(6).toString("hex")}.txt`);
    try {
      await writeFile(path, text, "utf8");
      const chatArgs = this.chat === null ? [] : ["--chat", this.chat];
      const result = await this.exec(["messages", "send", "--text-file", path, "--parse-mode", "none", ...chatArgs, "--confirm"]);
      return readSendResult(result);
    } catch (error) {
      return { ok: false, error: error instanceof Error ? error.message : "telegram send failed" };
    } finally {
      await rm(path, { force: true });
    }
  }
}

function readSendResult(result: ExecResult): SendResult {
  try {
    const envelope: unknown = JSON.parse(result.stdout);
    if (isObject(envelope) && envelope["ok"] === true) return { ok: true };
    const error = isObject(envelope) && isObject(envelope["error"]) ? envelope["error"] : {};
    const message = typeof error["message"] === "string" ? error["message"] : "telegram reported an error";
    return { ok: false, error: message };
  } catch {
    return { ok: false, error: `telegram exited ${result.exitCode} without a JSON envelope` };
  }
}

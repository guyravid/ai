import { execFile } from "node:child_process";
import type { Exec, ExecResult } from "./trello-client.ts";

const MAX_OUTPUT_BYTES = 64 * 1024 * 1024;

export const execTrello: Exec = (args) =>
  new Promise<ExecResult>((resolve) => {
    execFile("trello", args, { maxBuffer: MAX_OUTPUT_BYTES }, (error, stdout) => {
      const exitCode = error === null ? 0 : typeof error.code === "number" ? error.code : 1;
      resolve({ stdout, exitCode });
    });
  });

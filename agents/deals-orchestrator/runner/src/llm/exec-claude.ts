import { execFile } from "node:child_process";
import type { ClaudeExec } from "./claude-invocation.ts";

const MAX_OUTPUT_BYTES = 32 * 1024 * 1024;

export function createExecClaude(cwd: string): ClaudeExec {
  return (args) =>
    new Promise((resolve) => {
      execFile("claude", args, { cwd, maxBuffer: MAX_OUTPUT_BYTES }, (error, stdout) => {
        resolve({ stdout, exitCode: error === null ? 0 : typeof error.code === "number" ? error.code : 1 });
      });
    });
}

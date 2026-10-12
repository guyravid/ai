import type { Exec } from "./trello-client.ts";

export function okEnvelope(data: unknown, page?: { hasMore: boolean; nextCursor: string }): string {
  const meta =
    page === undefined
      ? { contract_version: "1.4.1", tool_version: "1.2.0" }
      : {
          contract_version: "1.4.1",
          tool_version: "1.2.0",
          page: { has_more: page.hasMore, next_cursor: page.nextCursor },
        };
  return JSON.stringify({ ok: true, tool: "trello", data, meta });
}

export function errorEnvelope(code: string, message: string, retriable = false): string {
  return JSON.stringify({ ok: false, tool: "trello", error: { code, message, retriable, exit_code: 1 } });
}

export interface FakeExec {
  exec: Exec;
  calls: string[][];
}

export function createFakeExec(respond: (args: string[]) => string): FakeExec {
  const calls: string[][] = [];
  const exec: Exec = async (args) => {
    calls.push(args);
    return { stdout: respond(args), exitCode: 0 };
  };
  return { exec, calls };
}

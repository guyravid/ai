import { createHash } from "node:crypto";
import { detectBotWall } from "./bot-wall.ts";
import type { HostRateLimiter } from "./rate-limiter.ts";

export const USER_AGENT = "deals-runner/0.1 (personal price tracker)";
export const DEFAULT_TIMEOUT_MS = 15_000;
export const DEFAULT_MAX_BYTES = 3 * 1024 * 1024;

export interface PreviousFetch {
  etag?: string | undefined;
  lastModified?: string | undefined;
  hash?: string | undefined;
}

export interface FetchDeps {
  fetch?: typeof fetch;
  limiter?: HostRateLimiter;
  timeoutMs?: number;
  maxBytes?: number;
}

export type FetchErrorKind = "timeout" | "http" | "network" | "too-large" | "blocked";

export type FetchResult =
  | { outcome: "not-modified" }
  | {
      outcome: "ok";
      body: string;
      finalUrl: string;
      httpStatus: number;
      etag?: string | undefined;
      lastModified?: string | undefined;
      hash: string;
    }
  | { outcome: "error"; kind: FetchErrorKind; httpStatus?: number | undefined; message: string };

const BLOCKED_STATUSES = [401, 403, 429, 503];

class TooLargeError extends Error {}

function conditionalHeaders(previous: PreviousFetch): Record<string, string> {
  const headers: Record<string, string> = {
    "User-Agent": USER_AGENT,
    Accept: "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5",
  };
  if (previous.etag !== undefined) headers["If-None-Match"] = previous.etag;
  if (previous.lastModified !== undefined) headers["If-Modified-Since"] = previous.lastModified;
  return headers;
}

async function readCappedBody(response: Response, maxBytes: number): Promise<Buffer> {
  const declared = Number(response.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > maxBytes) {
    await response.body?.cancel();
    throw new TooLargeError(`response declares ${declared} bytes, over the ${maxBytes} byte cap`);
  }
  if (response.body === null) return Buffer.alloc(0);

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxBytes) {
      await reader.cancel();
      throw new TooLargeError(`response exceeded the ${maxBytes} byte cap`);
    }
    chunks.push(value);
  }
  return Buffer.concat(chunks);
}

function classifyFailure(error: unknown): FetchResult {
  if (error instanceof TooLargeError) return { outcome: "error", kind: "too-large", message: error.message };
  const name = error instanceof Error ? error.name : "";
  const message = error instanceof Error ? error.message : "request failed";
  if (name === "TimeoutError" || name === "AbortError") {
    return { outcome: "error", kind: "timeout", message: "request timed out" };
  }
  return { outcome: "error", kind: "network", message };
}

export async function fetchPage(url: string, previous: PreviousFetch = {}, deps: FetchDeps = {}): Promise<FetchResult> {
  const doFetch = deps.fetch ?? fetch;
  const maxBytes = deps.maxBytes ?? DEFAULT_MAX_BYTES;
  try {
    await deps.limiter?.wait(new URL(url).hostname.toLowerCase());
    const response = await doFetch(url, {
      method: "GET",
      headers: conditionalHeaders(previous),
      redirect: "follow",
      signal: AbortSignal.timeout(deps.timeoutMs ?? DEFAULT_TIMEOUT_MS),
    });
    if (response.status === 304) {
      await response.body?.cancel();
      return { outcome: "not-modified" };
    }
    if (!response.ok) {
      await response.body?.cancel();
      return {
        outcome: "error",
        kind: BLOCKED_STATUSES.includes(response.status) ? "blocked" : "http",
        httpStatus: response.status,
        message: `HTTP ${response.status}`,
      };
    }
    const bytes = await readCappedBody(response, maxBytes);
    const body = bytes.toString("utf8");
    const botWall = detectBotWall(body, bytes.byteLength);
    if (botWall !== null) {
      return { outcome: "error", kind: "blocked", httpStatus: response.status, message: `bot wall detected (${botWall})` };
    }
    return {
      outcome: "ok",
      body,
      finalUrl: response.url === "" ? url : response.url,
      httpStatus: response.status,
      etag: response.headers.get("etag") ?? undefined,
      lastModified: response.headers.get("last-modified") ?? undefined,
      hash: createHash("sha256").update(bytes).digest("hex"),
    };
  } catch (error) {
    return classifyFailure(error);
  }
}

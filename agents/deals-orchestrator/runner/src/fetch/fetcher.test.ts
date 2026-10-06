import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { createHash } from "node:crypto";
import { fetchPage, USER_AGENT } from "./fetcher.ts";
import { HostRateLimiter, type Clock } from "./rate-limiter.ts";

interface Recorded {
  url: string;
  headers: Record<string, string>;
}

function fakeFetch(respond: (init: RequestInit) => Promise<Response> | Response) {
  const calls: Recorded[] = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    calls.push({ url: String(input), headers: { ...(init?.headers as Record<string, string>) } });
    return respond(init ?? {});
  };
  return { fetchImpl, calls };
}

describe("fetchPage", () => {
  it("returns the body, validators and sha256 hash on 200", async () => {
    const { fetchImpl, calls } = fakeFetch(
      () => new Response("<html>hi</html>", { status: 200, headers: { etag: '"v1"', "last-modified": "Tue, 01 Oct 2026 10:00:00 GMT" } }),
    );
    const result = await fetchPage("https://a.example/p", {}, { fetch: fetchImpl });
    assert.deepEqual(result, {
      outcome: "ok",
      body: "<html>hi</html>",
      finalUrl: "https://a.example/p",
      httpStatus: 200,
      etag: '"v1"',
      lastModified: "Tue, 01 Oct 2026 10:00:00 GMT",
      hash: createHash("sha256").update("<html>hi</html>").digest("hex"),
    });
    assert.equal(calls[0]?.headers["User-Agent"], USER_AGENT);
    assert.equal(calls[0]?.headers["If-None-Match"], undefined);
  });

  it("sends conditional headers and maps 304 to not-modified", async () => {
    const { fetchImpl, calls } = fakeFetch(() => new Response(null, { status: 304 }));
    const result = await fetchPage(
      "https://a.example/p",
      { etag: '"v1"', lastModified: "Tue, 01 Oct 2026 10:00:00 GMT" },
      { fetch: fetchImpl },
    );
    assert.deepEqual(result, { outcome: "not-modified" });
    assert.equal(calls[0]?.headers["If-None-Match"], '"v1"');
    assert.equal(calls[0]?.headers["If-Modified-Since"], "Tue, 01 Oct 2026 10:00:00 GMT");
  });

  it("maps 4xx and 5xx to http errors", async () => {
    for (const status of [404, 500]) {
      const { fetchImpl } = fakeFetch(() => new Response("nope", { status }));
      assert.deepEqual(await fetchPage("https://a.example/p", {}, { fetch: fetchImpl }), {
        outcome: "error",
        kind: "http",
        httpStatus: status,
        message: `HTTP ${status}`,
      });
    }
  });

  it("times out when the request hangs", async () => {
    const { fetchImpl } = fakeFetch(
      (init) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal?.addEventListener("abort", () => reject(init.signal?.reason));
        }),
    );
    const result = await fetchPage("https://a.example/p", {}, { fetch: fetchImpl, timeoutMs: 20 });
    assert.equal(result.outcome === "error" && result.kind, "timeout");
  });

  it("reports network failures", async () => {
    const { fetchImpl } = fakeFetch(() => {
      throw new TypeError("fetch failed");
    });
    const result = await fetchPage("https://a.example/p", {}, { fetch: fetchImpl });
    assert.deepEqual(result, { outcome: "error", kind: "network", message: "fetch failed" });
  });

  it("aborts bodies over the size cap", async () => {
    const { fetchImpl } = fakeFetch(() => new Response("x".repeat(1000), { status: 200 }));
    const result = await fetchPage("https://a.example/p", {}, { fetch: fetchImpl, maxBytes: 100 });
    assert.equal(result.outcome === "error" && result.kind, "too-large");
  });

  it("rejects early when content-length exceeds the cap", async () => {
    const { fetchImpl } = fakeFetch(() => new Response("tiny", { status: 200, headers: { "content-length": "5000" } }));
    const result = await fetchPage("https://a.example/p", {}, { fetch: fetchImpl, maxBytes: 100 });
    assert.equal(result.outcome === "error" && result.kind, "too-large");
  });

  it("accepts a body exactly at the cap", async () => {
    const { fetchImpl } = fakeFetch(() => new Response("x".repeat(100), { status: 200 }));
    const result = await fetchPage("https://a.example/p", {}, { fetch: fetchImpl, maxBytes: 100 });
    assert.equal(result.outcome, "ok");
  });
});

describe("HostRateLimiter", () => {
  function fakeClock(): { clock: Clock; sleeps: number[] } {
    let now = 1_000_000;
    const sleeps: number[] = [];
    const clock: Clock = {
      now: () => now,
      sleep: async (milliseconds) => {
        sleeps.push(milliseconds);
        now += milliseconds;
      },
    };
    return { clock, sleeps };
  }

  it("spaces requests to the same host and not across hosts", async () => {
    const { clock, sleeps } = fakeClock();
    const limiter = new HostRateLimiter({ minIntervalMs: 2000, clock });
    await limiter.wait("a.example");
    await limiter.wait("a.example");
    await limiter.wait("b.example");
    await limiter.wait("a.example");
    assert.deepEqual(sleeps, [2000, 2000]);
  });

  it("does not sleep when enough time has passed", async () => {
    const { clock, sleeps } = fakeClock();
    const limiter = new HostRateLimiter({ minIntervalMs: 2000, clock });
    await limiter.wait("a.example");
    await clock.sleep(5000);
    await limiter.wait("a.example");
    assert.deepEqual(sleeps, [5000]);
  });

  it("is applied by fetchPage", async () => {
    const { clock, sleeps } = fakeClock();
    const limiter = new HostRateLimiter({ minIntervalMs: 2000, clock });
    const { fetchImpl } = fakeFetch(() => new Response("ok"));
    await fetchPage("https://a.example/1", {}, { fetch: fetchImpl, limiter });
    await fetchPage("https://a.example/2", {}, { fetch: fetchImpl, limiter });
    assert.deepEqual(sleeps, [2000]);
  });
});

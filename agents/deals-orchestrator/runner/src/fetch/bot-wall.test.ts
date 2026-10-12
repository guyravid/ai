import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { describe, it } from "node:test";
import { detectBotWall } from "./bot-wall.ts";
import { fetchPage } from "./fetcher.ts";

function fixture(name: string): string {
  return readFileSync(new URL(`./fixtures/${name}`, import.meta.url), "utf8");
}

const WALLS: [string, string][] = [
  ["akamai-denied.html", "akamai-access-denied"],
  ["cloudflare-attention.html", "cloudflare-attention"],
  ["cloudflare-challenge.html", "cloudflare-challenge"],
  ["incapsula.html", "incapsula-interruption"],
  ["datadome.html", "datadome"],
  ["perimeterx.html", "perimeterx"],
  ["recaptcha-wall.html", "captcha-widget"],
];

describe("detectBotWall", () => {
  for (const [file, name] of WALLS) {
    it(`flags ${file}`, () => {
      const body = fixture(file);
      assert.equal(detectBotWall(body, Buffer.byteLength(body)), name);
    });
  }

  it("does not flag a large page that mentions captcha or access denied", () => {
    const body = `<html><body>${"<p>filler</p>".repeat(3000)}<p>captcha g-recaptcha Access Denied akamai</p></body></html>`;
    assert.ok(Buffer.byteLength(body) > 20 * 1024);
    assert.equal(detectBotWall(body, Buffer.byteLength(body)), null);
  });

  it("does not flag a small ordinary page", () => {
    const body = "<html><head><title>Kettle</title></head><body>Price $20. Solve a captcha later.</body></html>";
    assert.equal(detectBotWall(body, Buffer.byteLength(body)), null);
  });

  it("does not flag Access Denied without an akamai marker", () => {
    const body = "<html><title>Access Denied</title><body>Sign in first</body></html>";
    assert.equal(detectBotWall(body, Buffer.byteLength(body)), null);
  });
});

describe("fetchPage blocked classification", () => {
  it("maps 401, 403, 429 and 503 to blocked and keeps the status", async () => {
    for (const status of [401, 403, 429, 503]) {
      const result = await fetchPage("https://a.example/p", {}, { fetch: async () => new Response("x", { status }) });
      assert.deepEqual(result, { outcome: "error", kind: "blocked", httpStatus: status, message: `HTTP ${status}` });
    }
  });

  it("classifies a 200 bot wall as blocked", async () => {
    const result = await fetchPage(
      "https://a.example/p",
      {},
      { fetch: async () => new Response(fixture("akamai-denied.html"), { status: 200 }) },
    );
    assert.equal(result.outcome === "error" && result.kind, "blocked");
    assert.equal(result.outcome === "error" && result.httpStatus, 200);
  });

  it("returns a large normal 200 page as ok", async () => {
    const body = `<html>${"<p>filler</p>".repeat(3000)}<p>captcha</p></html>`;
    const result = await fetchPage("https://a.example/p", {}, { fetch: async () => new Response(body, { status: 200 }) });
    assert.equal(result.outcome, "ok");
  });
});

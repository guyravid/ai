// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { esc, fig, meterPositions, niceMax, renderReport } from '../lib/html.mjs';

const thresholds = { watchUsedPct: 70, atRiskUsedPct: 90, watchProjectedPct: 90, atRiskProjectedPct: 100 };

/**
 * @param {Partial<Record<string, any>>} overrides
 */
function usageFixture(overrides = {}) {
  /**
   * @param {string} key
   * @param {number} used
   * @param {number} projected
   * @param {string} status
   */
  const limit = (key, used, projected, status) => ({
    key, label: key, unit: key.startsWith('minutes') ? 'min' : 'GB', used, usedTag: '[billing API]',
    included: 2000, includedNote: '', usedPct: (used / 2000) * 100, projected, projectedTag: '[estimate]',
    projectedPct: (projected / 2000) * 100, status,
  });
  return {
    kind: 'usage', account: 'octo', plan: 'free', generatedAt: '2026-10-17T12:00:00.000Z',
    cycle: { start: '2026-10-01T00:00:00.000Z', end: '2026-11-01T00:00:00.000Z', daysInCycle: 31, daysElapsed: 16.5, daysLeft: 14.5 },
    thresholds, minElapsedDays: 1, notices: [], minutesSource: '[billing API]',
    limits: [limit('minutes-weighted', 1500, 2800, 'at risk'), limit('minutes-raw', 300, 560, 'fine'), limit('artifacts', 0, 0, 'fine'), limit('cache', 0.2, 0.2, 'fine')],
    byOs: [
      { os: 'macOS', raw: 120, weighted: 1240, multiplier: 10.33, gross: 7.44, covered: 7.44, hasDollars: true },
      { os: 'Linux', raw: 180, weighted: 180, multiplier: 1, gross: 1.08, covered: 1.08, hasDollars: true },
    ],
    daily: [{ date: '2026-10-03', raw: 100, weighted: 500 }, { date: '2026-10-16', raw: 200, weighted: 1000 }],
    dailySource: '[billing API]', storageFromBilling: false,
    privateRepos: [{ repo: 'octo/<script>alert(1)</script>', raw: 300, weighted: 1420, runs: 12, timingRaw: 300, workflows: [{ name: 'ci', path: '.github/workflows/ci.yml', raw: 300, weighted: 1420, jobs: [{ name: 'test (macos-latest)', raw: 120, weighted: 1240, attempts: 6 }] }] }],
    publicRepos: [], idle: { private: 2, public: 3 }, cacheByRepo: [],
    ...overrides,
  };
}

const riskFixture = {
  kind: 'risk', repo: 'octo/app', visibility: 'private', defaultBranch: 'main', repoCreatedAt: '2025-01-01T00:00:00Z',
  generatedAt: '2026-10-17T12:00:00.000Z', windowDays: 30, observedDays: 30, runs: 12, jobRuns: 24, rawTotal: 300,
  weightedTotal: 1420, monthlyNow: 1420, combinedSaving: 600, workflows: [{ path: '.github/workflows/ci.yml', ref: 'main' }], notes: [],
  findings: [{ kind: 'minutes', title: 'push + pull_request run twice', evidence: '.github/workflows/ci.yml:4 (@ main)', perRun: 118, perRunSource: 'timing', monthly: 600, change: 'on:\n  push:\n    branches: [main]', saving: 600 }],
  latent: [{ title: 'no paths filter', evidence: 'ci.yml:4', detail: 'details' }], ok: [],
};
const options = { sources: [{ label: 'billing', url: 'https://docs.github.com/en/billing' }], readDate: '2026-10-17' };

test('meter positions: thresholds, projection marker and overshoot scale', () => {
  const normal = meterPositions(50, 80, thresholds);
  assert.equal(normal.scaleMax, 100);
  assert.equal(normal.used, 50);
  assert.equal(normal.projected, 80);
  assert.equal(normal.watch, 70);
  assert.equal(normal.risk, 90);

  const over = meterPositions(95, 200, thresholds);
  assert.equal(over.scaleMax, 150); // capped
  assert.equal(over.projected, 100); // marker pinned at the right edge
  assert.ok(Math.abs(over.limit - 66.666) < 0.01);
  assert.ok(Math.abs(over.watch - 46.666) < 0.01);

  assert.equal(meterPositions(0, Infinity, thresholds).scaleMax, 150);
});

test('nice axis maximum', () => {
  assert.equal(niceMax(2080), 2500);
  assert.equal(niceMax(2000), 2000);
  assert.equal(niceMax(0.43), 0.5);
  assert.equal(niceMax(0), 1);
});

test('figures always carry a unit and a source tag', () => {
  assert.equal(fig(1234, 'min', '[billing API]'), '<span class="fig">1,234&nbsp;min <span class="src">[billing API]</span></span>');
  const html = renderReport(usageFixture(), riskFixture, { ...options, standalone: true });
  const figures = html.match(/<span class="fig">.*?<\/span><\/span>/g) ?? [];
  assert.ok(figures.length > 20);
  for (const figure of figures) assert.match(figure, /<span class="src">\[(billing API|run timing|estimate|docs)\]<\/span>/);
  assert.equal((html.match(/class="fig"/g) ?? []).length, figures.length);
});

test('worst status drives the headline pill', () => {
  const html = renderReport(usageFixture(), null, { ...options, standalone: true });
  const masthead = html.slice(html.indexOf('<header class="masthead">'), html.indexOf('</header>'));
  assert.match(masthead, /pill-risk/);
});

test('untrusted strings are escaped', () => {
  assert.equal(esc('<a href="x">&\'</a>'), '&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;');
  const html = renderReport(usageFixture(), riskFixture, { ...options, standalone: true });
  assert.ok(!html.includes('<script>alert(1)</script>'));
  assert.ok(html.includes('octo/&lt;script&gt;alert(1)&lt;/script&gt;'));
});

test('standalone file vs Artifact content', () => {
  const standalone = renderReport(usageFixture(), riskFixture, { ...options, standalone: true });
  const artifact = renderReport(usageFixture(), riskFixture, { ...options, standalone: false });
  assert.match(standalone, /^<!doctype html>/);
  assert.ok(!/<!doctype|<html|<head>|<body>/i.test(artifact));
  for (const html of [standalone, artifact]) {
    assert.ok(html.slice(0, 8192).includes('<title>octo Actions Usage</title>'));
    assert.ok(!/<link\b|src="https?:|url\(http/i.test(html), 'no external resources');
    assert.match(html, /@media \(prefers-color-scheme: dark\) \{ :root:not\(\[data-theme="light"\]\)/);
    assert.match(html, /:root\[data-theme="dark"\]/);
  }
});

test('risk section renders findings with the YAML change and combined saving', () => {
  const html = renderReport(usageFixture(), riskFixture, { ...options, standalone: true });
  assert.ok(html.includes('branches: [main]'));
  assert.ok(html.includes('Latent risks'));
  assert.match(html, /All minute changes together/);
});

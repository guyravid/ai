// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { parseYaml, lineOf } from '../lib/yaml.mjs';
import { allFilesMatch, combinedSaving, doubleTrigger, globToRegExp, jobNameMatcher, triggers } from '../lib/rules.mjs';

const quotas = JSON.parse(readFileSync(new URL('../lib/quotas.json', import.meta.url), 'utf8'));
const config = JSON.parse(readFileSync(new URL('../lib/config.json', import.meta.url), 'utf8'));

const ciYaml = `name: ci

on:
  push:
  pull_request:

concurrency:
  group: ci-\${{ github.ref }}   # comment
  cancel-in-progress: true

jobs:
  test:
    name: test (\${{ matrix.os }})
    runs-on: \${{ matrix.os }}
    timeout-minutes: 20
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest]
    steps:
      - uses: actions/checkout@v4
      - name: run
        run: |
          echo "# not a comment"
          make test
  install:
    runs-on: macos-latest
`;

test('yaml: workflow structure and key lines', () => {
  const doc = parseYaml(ciYaml);
  assert.deepEqual(triggers(doc.on), { push: {}, pull_request: {} });
  assert.equal(doc.concurrency.group, 'ci-${{ github.ref }}');
  assert.equal(doc.concurrency['cancel-in-progress'], true);
  assert.deepEqual(doc.jobs.test.strategy.matrix.os, ['ubuntu-latest', 'macos-latest']);
  assert.equal(doc.jobs.test.strategy['fail-fast'], false);
  assert.equal(doc.jobs.test.steps.length, 2);
  assert.match(doc.jobs.test.steps[1].run, /# not a comment/);
  assert.equal(lineOf(doc.on, 'push'), 4);
  assert.equal(lineOf(doc.jobs, 'install'), 26);
  assert.equal(lineOf(doc.jobs.test, 'runs-on'), 14);
});

test('triggers normalizes string, list and map forms', () => {
  assert.deepEqual(triggers('push'), { push: {} });
  assert.deepEqual(triggers(['push', 'pull_request']), { push: {}, pull_request: {} });
  assert.deepEqual(triggers({ push: { branches: ['main'] } }), { push: { branches: ['main'] } });
});

test('job name matcher: templated names and matrix defaults', () => {
  const doc = parseYaml(ciYaml);
  assert.ok(jobNameMatcher('test', doc.jobs.test).test('test (macos-latest)'));
  assert.ok(!jobNameMatcher('test', doc.jobs.test).test('install'));
  assert.ok(jobNameMatcher('install', doc.jobs.install).test('install'));
  assert.ok(jobNameMatcher('build', {}).test('build (ubuntu-latest, 20)'));
  assert.ok(jobNameMatcher('call', {}).test('call / inner'));
});

test('globs: docs-only detection', () => {
  const patterns = config.risk.docsOnlyPatterns;
  assert.ok(globToRegExp('**/*.md').test('README.md'));
  assert.ok(globToRegExp('**/*.md').test('docs/a/b.md'));
  assert.ok(globToRegExp('docs/**').test('docs/plans/0001.md'));
  assert.ok(!globToRegExp('docs/**').test('src/docs.go'));
  assert.ok(allFilesMatch(['README.md', 'docs/x.png'], patterns));
  assert.ok(!allFilesMatch(['README.md', 'main.go'], patterns));
  assert.ok(!allFilesMatch([], patterns));
});

/**
 * @param {number} runId
 * @param {string} jobName
 * @param {number} weightedMinutes
 */
const job = (runId, jobName, weightedMinutes) =>
  /** @type {any} */ ({ runId, runAttempt: 1, jobName, weightedMinutes, rawMinutes: weightedMinutes });

test('double trigger: push runs sharing a sha with a PR run are counted as duplicates', () => {
  const workflow = { path: '.github/workflows/ci.yml', ref: 'build/0001', doc: parseYaml(ciYaml) };
  const runs = /** @type {any[]} */ ([
    { id: 1, event: 'push', head_sha: 'a', head_branch: 'b', created_at: '2026-10-17T04:00:00Z' },
    { id: 2, event: 'push', head_sha: 'c', head_branch: 'b', created_at: '2026-10-17T13:00:00Z' },
    { id: 3, event: 'pull_request', head_sha: 'c', head_branch: 'b', created_at: '2026-10-17T13:00:02Z' },
  ]);
  const jobs = [job(1, 'test', 10), job(2, 'test', 20), job(3, 'test', 20)];
  const context = /** @type {any} */ ({ quotas, config, defaultBranch: 'main', scale: (/** @type {number} */ n) => n * 2 });
  const result = doubleTrigger(workflow, runs, jobs, context);
  assert.equal(result.findings.length, 1);
  assert.equal(result.findings[0].saving, 40); // run 2's 20 min, scaled x2
  assert.match(result.findings[0].change, /branches: \[main\]/);

  const limited = { ...workflow, doc: parseYaml(ciYaml.replace('  push:\n', '  push:\n    branches: [main]\n')) };
  assert.equal(doubleTrigger(limited, runs, jobs, context).findings.length, 0);
});

test('combined saving takes the largest claim per job, not the sum', () => {
  const jobs = [job(1, 'test', 10), job(2, 'test', 10)];
  const findings = /** @type {any[]} */ ([
    { kind: 'minutes', saving: 0, reductions: new Map([['1/1/test', 10]]) },
    { kind: 'minutes', saving: 0, reductions: new Map([['1/1/test', 4], ['2/1/test', 3]]) },
  ]);
  assert.equal(combinedSaving(findings, jobs, (n) => n), 13);
});

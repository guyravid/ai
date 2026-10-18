// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  billingCycle,
  costWeight,
  cronRunsIn30Days,
  failFastSavedMinutes,
  jobBilledMinutes,
  matrixSize,
  observedDays,
  percentOf,
  projectToCycleEnd,
  scaleTo30Days,
  skuForLabels,
  skuFromBillingName,
  statusFor,
} from '../lib/calc.mjs';

const quotas = JSON.parse(readFileSync(new URL('../lib/quotas.json', import.meta.url), 'utf8'));
const config = JSON.parse(readFileSync(new URL('../lib/config.json', import.meta.url), 'utf8'));
const close = (/** @type {number} */ actual, /** @type {number} */ expected) =>
  assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);

test('rounding: each job rounds up to the next whole minute', () => {
  assert.equal(jobBilledMinutes('2026-10-17T04:00:00Z', '2026-10-17T04:01:01Z'), 2); // 61 s
  assert.equal(jobBilledMinutes('2026-10-17T04:00:00Z', '2026-10-17T04:01:00Z'), 1); // exactly 60 s
  assert.equal(jobBilledMinutes('2026-10-17T04:00:00Z', '2026-10-17T04:00:01Z'), 1); // 1 s
  assert.equal(jobBilledMinutes('2026-10-17T04:00:00Z', '2026-10-17T04:00:00Z'), 0);
  assert.equal(jobBilledMinutes('2026-10-17T04:00:00Z', null), 0); // still running / skipped
});

test('rounding is per job, not per run: two 61 s jobs bill 4 min, not 3', () => {
  const jobs = [
    ['2026-10-17T04:00:00Z', '2026-10-17T04:01:01Z'],
    ['2026-10-17T04:00:00Z', '2026-10-17T04:01:01Z'],
  ];
  assert.equal(jobs.reduce((sum, [start, end]) => sum + jobBilledMinutes(start, end), 0), 4);
});

test('rounding matches the billing API for bellboard run 39001297865', () => {
  // Job timestamps from the jobs API; billing API reported 14 macOS min over this and two later runs.
  assert.equal(jobBilledMinutes('2026-10-17T04:06:18Z', '2026-10-17T04:10:15Z'), 4); // test (macos-latest)
  assert.equal(jobBilledMinutes('2026-10-17T04:06:18Z', '2026-10-17T04:06:56Z'), 1); // install
  assert.equal(jobBilledMinutes('2026-10-17T04:06:16Z', '2026-10-17T04:15:14Z'), 9); // test (ubuntu-latest)
});

test('cycle boundary: last instant of October vs first instant of November (UTC)', () => {
  const lastInstant = billingCycle(new Date('2026-10-31T23:59:59.999Z'));
  assert.equal(lastInstant.start.toISOString(), '2026-10-01T00:00:00.000Z');
  assert.equal(lastInstant.end.toISOString(), '2026-11-01T00:00:00.000Z');
  assert.equal(lastInstant.daysInCycle, 31);
  assert.ok(lastInstant.daysLeft > 0 && lastInstant.daysLeft < 1e-6);

  const firstInstant = billingCycle(new Date('2026-11-01T00:00:00.000Z'));
  assert.equal(firstInstant.start.toISOString(), '2026-11-01T00:00:00.000Z');
  assert.equal(firstInstant.daysInCycle, 30);
  assert.equal(firstInstant.daysElapsed, 0);
  assert.equal(firstInstant.daysLeft, 30);
});

test('cycle boundary: a local evening that is already the next month in UTC', () => {
  // 2026-10-31 20:30 at UTC-04:00 is 2026-11-01 00:30 UTC: November's cycle.
  const cycle = billingCycle(new Date('2026-10-31T20:30:00-04:00'));
  assert.equal(cycle.start.toISOString(), '2026-11-01T00:00:00.000Z');
});

test('cycle length follows the month, including leap February', () => {
  assert.equal(billingCycle(new Date('2027-02-10T00:00:00Z')).daysInCycle, 28);
  assert.equal(billingCycle(new Date('2028-02-10T00:00:00Z')).daysInCycle, 29);
  assert.equal(billingCycle(new Date('2026-12-31T12:00:00Z')).end.toISOString(), '2027-01-01T00:00:00.000Z');
});

test('projection: linear at the average daily rate', () => {
  close(projectToCycleEnd(100, 10, 31, 1), 310);
  close(projectToCycleEnd(170, 16.7, 31, 1), (170 / 16.7) * 31);
});

test('projection: elapsed time is clamped so the first hours of a cycle do not explode', () => {
  close(projectToCycleEnd(10, 0, 30, 1), 300); // zero elapsed -> treated as 1 day
  close(projectToCycleEnd(10, 0.04, 30, 1), 300);
  assert.equal(projectToCycleEnd(0, 0, 30, 1), 0);
});

test('projection: at or past cycle end it is the used amount', () => {
  assert.equal(projectToCycleEnd(500, 31, 31, 1), 500);
});

test('status thresholds at their edges', () => {
  const thresholds = config.status;
  assert.equal(statusFor(69.9, 89.9, thresholds), 'fine');
  assert.equal(statusFor(70, 10, thresholds), 'watch');
  assert.equal(statusFor(10, 90, thresholds), 'watch');
  assert.equal(statusFor(10, 100, thresholds), 'watch');
  assert.equal(statusFor(10, 100.1, thresholds), 'at risk');
  assert.equal(statusFor(90, 10, thresholds), 'at risk');
  assert.equal(statusFor(89.9, 95, thresholds), 'watch');
});

test('percentOf handles a zero allowance', () => {
  assert.equal(percentOf(5, 0), Infinity);
  assert.equal(percentOf(0, 0), 0);
  assert.equal(percentOf(500, 2000), 25);
});

test('monthly scaling from the observed window', () => {
  assert.equal(scaleTo30Days(10, 30, 1), 10);
  assert.equal(scaleTo30Days(3, 4.7, 1), (3 * 30) / 4.7);
  assert.equal(scaleTo30Days(2, 0.1, 1), 60); // clamped to 1 day
});

test('observed days: window, or since repo creation when younger', () => {
  const now = new Date('2026-10-17T17:30:00Z');
  assert.equal(observedDays(now, 30, null), 30);
  assert.equal(observedDays(now, 30, new Date('2025-01-01T00:00:00Z')), 30);
  close(observedDays(now, 30, new Date('2026-10-13T01:17:55Z')), (now.getTime() - Date.parse('2026-10-13T01:17:55Z')) / 86_400_000);
});

test('cost weight comes from documented per-minute rates', () => {
  close(costWeight(quotas, 'actions_linux'), 1);
  close(costWeight(quotas, 'actions_macos'), 0.062 / 0.006);
  close(costWeight(quotas, 'actions_windows'), 0.01 / 0.006);
  close(costWeight(quotas, 'linux_4_core'), 0.012 / 0.006);
});

test('runner labels and billing SKU names map to SKUs', () => {
  assert.equal(skuForLabels(quotas, ['macos-latest']), 'actions_macos');
  assert.equal(skuForLabels(quotas, ['ubuntu-24.04-arm']), 'actions_linux_arm');
  assert.equal(skuForLabels(quotas, ['self-hosted', 'linux']), 'self-hosted');
  assert.equal(skuForLabels(quotas, ['my-custom-runner']), 'unknown');
  assert.equal(skuFromBillingName('Actions Linux'), 'actions_linux');
  assert.equal(skuFromBillingName('Actions macOS 3-core'), 'actions_macos');
  assert.equal(skuFromBillingName('Actions Windows ARM'), 'actions_windows_arm');
  assert.equal(skuFromBillingName('Actions Linux 1-core'), 'actions_linux_slim');
});

test('matrix size: product, exclude, include', () => {
  assert.equal(matrixSize({ os: ['ubuntu-latest', 'macos-latest'] }), 2);
  assert.equal(matrixSize({ os: ['a', 'b'], node: [18, 20, 22] }), 6);
  assert.equal(matrixSize({ os: ['a', 'b'], node: [18, 20], exclude: [{ os: 'a', node: 18 }] }), 3);
  assert.equal(matrixSize({ os: ['a', 'b'], include: [{ os: 'c' }] }), 3); // new value -> new job
  assert.equal(matrixSize({ os: ['a', 'b'], include: [{ os: 'a', extra: 1 }] }), 2); // extends a
  assert.equal(matrixSize({ os: ['a', 'b'], include: [{ extra: 1 }] }), 2); // extends every job
  assert.equal(matrixSize({ include: [{ os: 'a' }, { os: 'b' }] }), 2);
});

test('cron runs per 30 days', () => {
  const from = new Date('2026-10-01T00:00:00Z'); // a Thursday
  assert.equal(cronRunsIn30Days('0 * * * *', from), 720);
  assert.equal(cronRunsIn30Days('0 6 * * *', from), 30);
  assert.equal(cronRunsIn30Days('*/15 * * * *', from), 30 * 24 * 4);
  assert.equal(cronRunsIn30Days('0 6 * * 1-5', from), 22); // Oct 1-30 2026 has 22 weekdays
  assert.equal(cronRunsIn30Days('0 0 1 * *', from), 1);
  assert.ok(Number.isNaN(cronRunsIn30Days('@daily', from)));
});

test('fail-fast saving: sibling legs cut at the first failure, weighted', () => {
  // bellboard PR run 39036203875: macOS leg failed at 13:24:23; the Linux leg ran until 13:30:15.
  const legs = [
    { startedAt: '2026-10-17T13:21:19Z', completedAt: '2026-10-17T13:24:23Z', conclusion: 'failure', weight: 0.062 / 0.006 },
    { startedAt: '2026-10-17T13:21:16Z', completedAt: '2026-10-17T13:30:15Z', conclusion: 'failure', weight: 1 },
  ];
  // Linux leg: 9 min billed in full, 4 min if cancelled at 13:24:23 (3 min 7 s rounded up) -> 5 saved.
  assert.equal(failFastSavedMinutes(legs), 5);
  assert.equal(failFastSavedMinutes([{ ...legs[1], conclusion: 'success' }]), 0);
});

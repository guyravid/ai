// @ts-check
// Pure arithmetic for the github-actions-usage skill. No I/O: every rate comes in through `quotas`
// (scripts/lib/quotas.json) and every threshold through `config` (scripts/lib/config.json).

const MS_PER_MINUTE = 60_000;
const MS_PER_DAY = 86_400_000;

/**
 * @typedef {{ standardRunnerRates: Record<string, number>, largerRunnerRates: Record<string, number>,
 *   baselineSku: string, labelToSku: Record<string, string>, skuOs: Record<string, string> }} Quotas
 * @typedef {{ watchUsedPct: number, atRiskUsedPct: number, watchProjectedPct: number, atRiskProjectedPct: number }} StatusThresholds
 * @typedef {'fine' | 'watch' | 'at risk'} Status
 */

/**
 * Billed minutes for one job: GitHub rounds each job's duration up to the next whole minute.
 * @param {string | null | undefined} startedAt
 * @param {string | null | undefined} completedAt
 * @returns {number}
 */
export function jobBilledMinutes(startedAt, completedAt) {
  if (!startedAt || !completedAt) return 0;
  const durationMs = Date.parse(completedAt) - Date.parse(startedAt);
  if (!Number.isFinite(durationMs) || durationMs <= 0) return 0;
  return Math.ceil(durationMs / MS_PER_MINUTE);
}

/**
 * @param {Quotas} quotas
 * @param {string} sku
 * @returns {number | undefined}
 */
export function rateFor(quotas, sku) {
  return quotas.standardRunnerRates[sku] ?? quotas.largerRunnerRates[sku];
}

/**
 * Cost-weighted multiplier: the SKU's per-minute rate divided by the baseline (Linux 2-core) rate.
 * Derived, not documented (see references/quotas.md, flag 1).
 * @param {Quotas} quotas
 * @param {string} sku
 * @returns {number}
 */
export function costWeight(quotas, sku) {
  const rate = rateFor(quotas, sku);
  const baseline = quotas.standardRunnerRates[quotas.baselineSku];
  if (rate === undefined || !baseline) return 1;
  return rate / baseline;
}

/**
 * @param {Quotas} quotas
 * @param {string[]} labels runs-on labels of a job
 * @returns {string} SKU, 'self-hosted', or 'unknown'
 */
export function skuForLabels(quotas, labels) {
  if (labels.includes('self-hosted')) return 'self-hosted';
  for (const label of labels) {
    const sku = quotas.labelToSku[label];
    if (sku) return sku;
  }
  return 'unknown';
}

/**
 * Billing API SKU names are display names ("Actions Linux", "Actions macOS 3-core"). Match by keyword.
 * @param {string} billingSku
 * @returns {string} internal SKU id, or 'unknown'
 */
export function skuFromBillingName(billingSku) {
  const name = billingSku.toLowerCase();
  const isArm = /\barm/.test(name);
  if (name.includes('macos')) return 'actions_macos';
  if (name.includes('windows')) return isArm ? 'actions_windows_arm' : 'actions_windows';
  if (name.includes('linux')) {
    if (name.includes('slim') || /1-core/.test(name)) return 'actions_linux_slim';
    return isArm ? 'actions_linux_arm' : 'actions_linux';
  }
  return 'unknown';
}

/**
 * The metered cycle is the calendar month in UTC: 00:00:00 on the 1st to the start of the next month.
 * @param {Date} now
 * @returns {{ start: Date, end: Date, daysInCycle: number, daysElapsed: number, daysLeft: number }}
 */
export function billingCycle(now) {
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1));
  const daysInCycle = (end.getTime() - start.getTime()) / MS_PER_DAY;
  const daysElapsed = (now.getTime() - start.getTime()) / MS_PER_DAY;
  return { start, end, daysInCycle, daysElapsed, daysLeft: daysInCycle - daysElapsed };
}

/**
 * Linear projection to cycle end at the average daily rate since the cycle started. Elapsed time is
 * clamped to `minElapsedDays` so a few hours into a new cycle doesn't produce a wild extrapolation.
 * @param {number} used
 * @param {number} daysElapsed
 * @param {number} daysInCycle
 * @param {number} minElapsedDays
 * @returns {number}
 */
export function projectToCycleEnd(used, daysElapsed, daysInCycle, minElapsedDays) {
  const elapsed = Math.max(daysElapsed, minElapsedDays);
  if (elapsed >= daysInCycle) return used;
  return (used / elapsed) * daysInCycle;
}

/**
 * @param {number} part
 * @param {number} whole
 * @returns {number}
 */
export function percentOf(part, whole) {
  if (whole <= 0) return part > 0 ? Infinity : 0;
  return (part / whole) * 100;
}

/**
 * fine: under watch on both; watch: used >= watchUsedPct or projected in [watchProjectedPct, atRiskProjectedPct];
 * at risk: used >= atRiskUsedPct or projected > atRiskProjectedPct. The worst condition wins.
 * @param {number} usedPct
 * @param {number} projectedPct
 * @param {StatusThresholds} thresholds
 * @returns {Status}
 */
export function statusFor(usedPct, projectedPct, thresholds) {
  if (usedPct >= thresholds.atRiskUsedPct || projectedPct > thresholds.atRiskProjectedPct) return 'at risk';
  if (usedPct >= thresholds.watchUsedPct || projectedPct >= thresholds.watchProjectedPct) return 'watch';
  return 'fine';
}

/**
 * Scale a count observed over `observedDays` to a 30-day month.
 * @param {number} count
 * @param {number} observedDays
 * @param {number} minObservedDays
 * @returns {number}
 */
export function scaleTo30Days(count, observedDays, minObservedDays) {
  return (count * 30) / Math.max(observedDays, minObservedDays);
}

/**
 * Days of history the 30-day run rate is based on: from the later of (now - windowDays) and the repo's
 * creation, to now.
 * @param {Date} now
 * @param {number} windowDays
 * @param {Date | null} repoCreatedAt
 * @returns {number}
 */
export function observedDays(now, windowDays, repoCreatedAt) {
  const windowStart = now.getTime() - windowDays * MS_PER_DAY;
  const start = repoCreatedAt ? Math.max(windowStart, repoCreatedAt.getTime()) : windowStart;
  return (now.getTime() - start) / MS_PER_DAY;
}

/**
 * Number of matrix jobs a strategy.matrix expands to: product of list-valued keys, minus `exclude`
 * entries, plus `include` entries that add a new combination.
 * @param {Record<string, unknown>} matrix
 * @returns {number}
 */
export function matrixSize(matrix) {
  const dimensions = Object.entries(matrix).filter(
    ([key, value]) => key !== 'include' && key !== 'exclude' && Array.isArray(value),
  );
  const base = dimensions.reduce((product, [, value]) => product * /** @type {unknown[]} */ (value).length, 1);
  const include = Array.isArray(matrix.include) ? matrix.include : [];
  const exclude = Array.isArray(matrix.exclude) ? matrix.exclude.length : 0;
  if (dimensions.length === 0) return include.length;
  // An include entry only adds a job when one of its original-matrix values is new; otherwise it
  // extends existing combinations.
  const addedByInclude = include.filter(
    (entry) =>
      entry &&
      typeof entry === 'object' &&
      dimensions.some(([key, values]) => key in entry && !/** @type {unknown[]} */ (values).includes(entry[key])),
  ).length;
  return Math.max(base - exclude, 0) + addedByInclude;
}

/**
 * Parse one cron field into the set of allowed values.
 * @param {string} field
 * @param {number} min
 * @param {number} max
 * @returns {Set<number>}
 */
function cronField(field, min, max) {
  const values = new Set();
  for (const part of field.split(',')) {
    const [rangePart, stepPart] = part.split('/');
    const step = stepPart ? Number(stepPart) : 1;
    let low = min;
    let high = max;
    if (rangePart !== '*') {
      const [a, b] = rangePart.split('-').map(Number);
      low = a;
      high = b ?? (stepPart ? max : a);
    }
    for (let value = low; value <= high; value += step) values.add(value);
  }
  return values;
}

/**
 * How many times a 5-field cron schedule (UTC) fires in the 30 days starting at `from`.
 * Day-of-month and day-of-week follow cron's OR rule when both are restricted.
 * @param {string} expression
 * @param {Date} from
 * @returns {number}
 */
export function cronRunsIn30Days(expression, from) {
  const fields = expression.trim().split(/\s+/);
  if (fields.length !== 5) return NaN;
  const minutes = cronField(fields[0], 0, 59);
  const hours = cronField(fields[1], 0, 23);
  const daysOfMonth = cronField(fields[2], 1, 31);
  const months = cronField(fields[3], 1, 12);
  const daysOfWeek = cronField(fields[4].replace(/\b7\b/, '0'), 0, 6);
  const domRestricted = fields[2] !== '*';
  const dowRestricted = fields[4] !== '*';
  let count = 0;
  const startDay = Date.UTC(from.getUTCFullYear(), from.getUTCMonth(), from.getUTCDate());
  for (let day = 0; day < 30; day++) {
    const date = new Date(startDay + day * MS_PER_DAY);
    if (!months.has(date.getUTCMonth() + 1)) continue;
    const domMatch = daysOfMonth.has(date.getUTCDate());
    const dowMatch = daysOfWeek.has(date.getUTCDay());
    const dayMatches = domRestricted && dowRestricted ? domMatch || dowMatch : domMatch && dowMatch;
    if (dayMatches) count += hours.size * minutes.size;
  }
  return count;
}

/**
 * Bytes to binary GB (2^30), as the docs measure storage.
 * @param {number} bytes
 * @param {number} bytesPerGB
 * @returns {number}
 */
export function bytesToGB(bytes, bytesPerGB) {
  return bytes / bytesPerGB;
}

/**
 * Minutes a job would not have billed had it been cancelled at `cancelAt` (rounded up per job, as
 * billing does), times its weight. Zero when it finished before `cancelAt`.
 * @param {{ startedAt: string, completedAt: string, weight?: number }} job
 * @param {number} cancelAt epoch ms
 * @returns {number}
 */
export function minutesSavedByCancelling(job, cancelAt) {
  if (Date.parse(job.completedAt) <= cancelAt) return 0;
  const billedFull = jobBilledMinutes(job.startedAt, job.completedAt);
  const cutAt = new Date(Math.max(cancelAt, Date.parse(job.startedAt))).toISOString();
  return (billedFull - jobBilledMinutes(job.startedAt, cutAt)) * (job.weight ?? 1);
}

/**
 * Minutes a fail-fast matrix would have saved in one run: each sibling leg still running when the
 * first leg failed is cancelled at that moment.
 * @param {{ startedAt: string, completedAt: string, conclusion: string | null, weight?: number }[]} legs
 * @returns {number} minutes saved, weighted by each leg's `weight` (default 1)
 */
export function failFastSavedMinutes(legs) {
  const failures = legs
    .filter((leg) => leg.conclusion === 'failure')
    .map((leg) => Date.parse(leg.completedAt));
  if (failures.length === 0) return 0;
  const firstFailure = Math.min(...failures);
  return legs.reduce((saved, leg) => saved + minutesSavedByCancelling(leg, firstFailure), 0);
}

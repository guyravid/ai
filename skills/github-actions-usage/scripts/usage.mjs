#!/usr/bin/env node
// @ts-check
// GitHub Actions usage for the current billing cycle: minutes and storage against the plan's included
// amounts, projection to cycle end, and a per-repository / workflow / job breakdown. Read-only.
//
// Usage: node usage.mjs [--user <login>] [--plan free|pro|team|free_org|enterprise] [--now <ISO date>]
//                       [--json <file>]   also write the computed report as JSON (for report-html.mjs)
import { readFileSync, writeFileSync } from 'node:fs';
import { parseArgs } from 'node:util';
import {
  billingCycle,
  bytesToGB,
  costWeight,
  percentOf,
  projectToCycleEnd,
  skuFromBillingName,
  statusFor,
} from './lib/calc.mjs';
import { GhError, apiGet, apiGetAll, ghJson, missingScopeNotice, tokenScopes } from './lib/gh.mjs';
import { jobRecords, listRuns } from './lib/history.mjs';
import { TAG, day, gigabytes, minutes, num, percent, table } from './lib/format.mjs';

const quotas = JSON.parse(readFileSync(new URL('./lib/quotas.json', import.meta.url), 'utf8'));
const config = JSON.parse(readFileSync(new URL('./lib/config.json', import.meta.url), 'utf8'));

const { values: args } = parseArgs({
  options: { user: { type: 'string' }, plan: { type: 'string' }, now: { type: 'string' }, json: { type: 'string' } },
});

const now = args.now ? new Date(args.now) : new Date();
const cycle = billingCycle(now);
/** @type {string[]} */
const notices = [];

const scopes = tokenScopes();
const hasUserScope = scopes === null || scopes.includes('user');
if (scopes === null) {
  notices.push('NOTICE: could not read the token scopes (no X-OAuth-Scopes header). Trying the billing API anyway.');
} else if (!hasUserScope) {
  notices.push(
    missingScopeNotice(
      'user',
      'Without it the billing usage API and the account plan are unavailable; minutes fall back to [run timing] and the plan to --plan or "free".',
    ),
  );
}

const authUser = apiGet('user');
const login = args.user ?? authUser.login;
const planName = resolvePlan();
const plan = quotas.plans[planName];

/** @returns {string} */
function resolvePlan() {
  if (args.plan) return args.plan;
  const name = authUser.plan?.name;
  if (name && quotas.plans[name]) return name;
  notices.push(`NOTICE: plan not readable from the API${name ? ` ("${name}" is not in quotas.json)` : ''}; assuming "free". Pass --plan to override.`);
  return 'free';
}

// --- Billing API -----------------------------------------------------------------------------
/** @type {{ product: string, sku: string, quantity: number, unitType: string, grossAmount: number, discountAmount: number, netAmount: number, repositoryName?: string, date: string }[] | null} */
let billingItems = null;
if (hasUserScope) {
  try {
    const response = apiGet(
      `users/${login}/settings/billing/usage?year=${now.getUTCFullYear()}&month=${now.getUTCMonth() + 1}`,
    );
    billingItems = (response.usageItems ?? []).filter((/** @type {{ product: string }} */ item) => item.product === 'actions');
  } catch (error) {
    if (!(error instanceof GhError)) throw error;
    const scope = error.missingScope;
    notices.push(
      scope
        ? missingScopeNotice(scope, 'The billing usage API refused the request; minutes fall back to [run timing].')
        : `NOTICE: billing usage API unavailable (${error.isNotFound ? 'HTTP 404: account may not be on the enhanced billing platform' : error.message.split('\n')[0]}); minutes fall back to [run timing].`,
    );
  }
}

// --- Repositories and run history this cycle --------------------------------------------------
/** @type {{ nameWithOwner: string, name: string, visibility: string, isArchived: boolean }[]} */
const repos = ghJson(['repo', 'list', login, '--limit', '1000', '--json', 'nameWithOwner,name,visibility,isArchived']);
const privateRepos = repos.filter((repo) => repo.visibility !== 'PUBLIC');
const publicRepos = repos.filter((repo) => repo.visibility === 'PUBLIC');

/** @type {Map<string, import('./lib/history.mjs').JobRecord[]>} */
const jobsByRepo = new Map();
/** @type {Map<string, number>} */
const runCountByRepo = new Map();
for (const repo of repos) {
  if (repo.isArchived) continue;
  const runs = listRuns(repo.nameWithOwner, cycle.start).filter((run) => Date.parse(run.created_at) < now.getTime());
  if (runs.length === 0) continue;
  runCountByRepo.set(repo.nameWithOwner, runs.length);
  jobsByRepo.set(repo.nameWithOwner, jobRecords(repo.nameWithOwner, runs, quotas));
}

// --- Minutes ----------------------------------------------------------------------------------
/** @type {Map<string, { raw: number, gross: number, covered: number, net: number }>} */
const minutesBySku = new Map();
/** @type {Map<string, Map<string, number>>} raw minutes per repo name per SKU from billing */
const billingRepoSku = new Map();
const minutesSource = billingItems ? TAG.billing : TAG.timing;

/** @type {Map<string, { raw: number, weighted: number }>} minutes per UTC day */
const dailyMinutes = new Map();
/**
 * @param {string} date ISO timestamp
 * @param {number} raw
 * @param {number} weighted
 */
const addDaily = (date, raw, weighted) => {
  const key = date.slice(0, 10);
  const entry = dailyMinutes.get(key) ?? { raw: 0, weighted: 0 };
  entry.raw += raw;
  entry.weighted += weighted;
  dailyMinutes.set(key, entry);
};

if (billingItems) {
  for (const item of billingItems.filter((entry) => entry.unitType === 'Minutes')) {
    const sku = skuFromBillingName(item.sku);
    addDaily(item.date, item.quantity, item.quantity * costWeight(quotas, sku));
    const bucket = minutesBySku.get(sku) ?? { raw: 0, gross: 0, covered: 0, net: 0 };
    bucket.raw += item.quantity;
    bucket.gross += item.grossAmount;
    bucket.covered += item.discountAmount;
    bucket.net += item.netAmount;
    minutesBySku.set(sku, bucket);
    const repoName = item.repositoryName ?? '(unattributed)';
    const perRepo = billingRepoSku.get(repoName) ?? new Map();
    perRepo.set(sku, (perRepo.get(sku) ?? 0) + item.quantity);
    billingRepoSku.set(repoName, perRepo);
  }
} else {
  for (const repo of privateRepos) {
    for (const job of jobsByRepo.get(repo.nameWithOwner) ?? []) {
      if (job.sku === 'self-hosted') continue;
      if (job.completedAt) addDaily(job.completedAt, job.rawMinutes, job.weightedMinutes);
      const bucket = minutesBySku.get(job.sku) ?? { raw: 0, gross: 0, covered: 0, net: 0 };
      bucket.raw += job.rawMinutes;
      minutesBySku.set(job.sku, bucket);
      const perRepo = billingRepoSku.get(repo.name) ?? new Map();
      perRepo.set(job.sku, (perRepo.get(job.sku) ?? 0) + job.rawMinutes);
      billingRepoSku.set(repo.name, perRepo);
    }
  }
}

let rawTotal = 0;
let weightedTotal = 0;
/** @type {Map<string, { raw: number, weighted: number, gross: number, covered: number }>} */
const byOs = new Map();
for (const [sku, bucket] of minutesBySku) {
  const weight = costWeight(quotas, sku);
  rawTotal += bucket.raw;
  weightedTotal += bucket.raw * weight;
  const os = quotas.skuOs[sku] ?? sku;
  const entry = byOs.get(os) ?? { raw: 0, weighted: 0, gross: 0, covered: 0 };
  entry.raw += bucket.raw;
  entry.weighted += bucket.raw * weight;
  entry.gross += bucket.gross;
  entry.covered += bucket.covered;
  byOs.set(os, entry);
}

const minElapsed = config.projection.minElapsedDays;
const weightedProjected = projectToCycleEnd(weightedTotal, cycle.daysElapsed, cycle.daysInCycle, minElapsed);
const rawProjected = projectToCycleEnd(rawTotal, cycle.daysElapsed, cycle.daysInCycle, minElapsed);

// --- Storage ----------------------------------------------------------------------------------
const bytesPerGB = quotas.storage.bytesPerGB;
const storageItems = (billingItems ?? []).filter((item) => item.unitType !== 'Minutes');
const hoursElapsed = Math.max(cycle.daysElapsed, minElapsed) * 24;
const hoursInCycle = cycle.daysInCycle * 24;

let artifactUsedGB;
let artifactProjectedGB;
let artifactSource;
if (storageItems.length > 0) {
  const gbHours = storageItems
    .filter((item) => /hour/i.test(item.unitType) && !/cache/i.test(item.sku))
    .reduce((sum, item) => sum + item.quantity, 0);
  artifactUsedGB = gbHours / hoursInCycle;
  artifactProjectedGB = gbHours / Math.min(hoursElapsed, hoursInCycle);
  artifactSource = TAG.billing;
} else {
  let bytes = 0;
  for (const repo of privateRepos) {
    if (repo.isArchived) continue;
    const artifacts = apiGetAll(`repos/${repo.nameWithOwner}/actions/artifacts?per_page=100`, 'artifacts');
    bytes += artifacts.filter((artifact) => !artifact.expired).reduce((sum, artifact) => sum + artifact.size_in_bytes, 0);
  }
  artifactUsedGB = bytesToGB(bytes, bytesPerGB);
  artifactProjectedGB = artifactUsedGB;
  artifactSource = TAG.estimate;
}

/** @type {{ repo: string, gb: number, count: number }[]} */
const cacheByRepo = [];
for (const repo of repos) {
  if (repo.isArchived) continue;
  const usage = apiGet(`repos/${repo.nameWithOwner}/actions/cache/usage`);
  if (usage.active_caches_count > 0) {
    cacheByRepo.push({ repo: repo.nameWithOwner, gb: bytesToGB(usage.active_caches_size_in_bytes, bytesPerGB), count: usage.active_caches_count });
  }
}
cacheByRepo.sort((a, b) => b.gb - a.gb);
const largestCache = cacheByRepo[0] ?? { repo: 'none', gb: 0, count: 0 };

// --- Report -----------------------------------------------------------------------------------
const thresholds = config.status;
/**
 * @param {string} label
 * @param {string} used
 * @param {string} included
 * @param {number} usedPct
 * @param {string} projected
 * @param {number} projectedPct
 * @returns {string[]}
 */
const summaryRow = (label, used, included, usedPct, projected, projectedPct) => [
  label,
  used,
  included,
  percent(usedPct),
  projected,
  statusFor(usedPct, projectedPct, thresholds),
];

const includedMinutes = plan.includedMinutes;
const out = [];
out.push(
  `GitHub Actions usage - ${login} (plan: ${planName}) - cycle ${day(cycle.start)} to ${day(new Date(cycle.end.getTime() - 1))} (${num(cycle.daysLeft, 1)} days left)`,
);
out.push(`Generated ${now.toISOString()}. Cycle = calendar month, UTC.`);
if (notices.length > 0) out.push('', ...notices);
out.push('');
out.push(
  table(
    ['Limit', 'Used', 'Included', '%', 'Projected at cycle end', 'Status'],
    [
      summaryRow(
        'Minutes (cost-weighted)',
        `${minutes(weightedTotal)} ${TAG.estimate}`,
        `${minutes(includedMinutes)}`,
        percentOf(weightedTotal, includedMinutes),
        `${minutes(weightedProjected)} ${TAG.estimate}`,
        percentOf(weightedProjected, includedMinutes),
      ),
      summaryRow(
        'Minutes (raw)',
        `${minutes(rawTotal)} ${minutesSource}`,
        `${minutes(includedMinutes)}`,
        percentOf(rawTotal, includedMinutes),
        `${minutes(rawProjected)} ${TAG.estimate}`,
        percentOf(rawProjected, includedMinutes),
      ),
      summaryRow(
        'Artifact storage',
        `${gigabytes(artifactUsedGB)} ${artifactSource}`,
        `${gigabytes(plan.artifactStorageGB)}`,
        percentOf(artifactUsedGB, plan.artifactStorageGB),
        `${gigabytes(artifactProjectedGB)} ${TAG.estimate}`,
        percentOf(artifactProjectedGB, plan.artifactStorageGB),
      ),
      summaryRow(
        'Cache (largest repo)',
        `${gigabytes(largestCache.gb)} ${TAG.estimate}`,
        `${gigabytes(plan.cacheStoragePerRepoGB)}/repo`,
        percentOf(largestCache.gb, plan.cacheStoragePerRepoGB),
        `${gigabytes(largestCache.gb)} ${TAG.estimate}`,
        percentOf(largestCache.gb, plan.cacheStoragePerRepoGB),
      ),
    ],
  ),
);
out.push('');
out.push('Notes:');
out.push(
  '- Included minutes are stated in minutes, but GitHub no longer documents how a macOS or Windows minute counts against them.',
  '  Cost-weighted = raw minutes x (runner rate / Linux 2-core rate); status uses it as the conservative figure. See references/quotas.md, flag 1.',
);
out.push(
  storageItems.length > 0
    ? '- Artifact storage: GB-months accrued so far from billing GB-hours; projection = average GB so far held to cycle end.'
    : '- Artifact storage: no storage rows in the billing API this cycle, so this is a snapshot of unexpired artifacts in private repos (artifacts API), assumed constant. GitHub Packages shares this allowance and is not counted.',
);
out.push(`- Cache: largest repo is ${largestCache.repo}; snapshot of active caches (cache usage API). Caches unused for ${quotas.storage.cacheEvictionUnusedDays} days are evicted.`);
out.push(`- Projection: used / days elapsed (min ${minElapsed} day) x ${num(cycle.daysInCycle)} days in cycle.`);
out.push(`- Status: fine < ${thresholds.watchUsedPct}% used and projected < ${thresholds.watchProjectedPct}%; watch >= ${thresholds.watchUsedPct}% or projected ${thresholds.watchProjectedPct}-${thresholds.atRiskProjectedPct}%; at risk >= ${thresholds.atRiskUsedPct}% or projected > ${thresholds.atRiskProjectedPct}% (scripts/lib/config.json).`);

out.push('');
out.push(`Minutes by runner OS ${minutesSource}`);
const osRows = [...byOs.entries()]
  .sort((a, b) => b[1].weighted - a[1].weighted)
  .map(([os, entry]) => [
    os,
    `${minutes(entry.raw)} ${minutesSource}`,
    `${num(entry.raw > 0 ? entry.weighted / entry.raw : 0, 2)}x ${TAG.estimate}`,
    `${minutes(entry.weighted)} ${TAG.estimate}`,
    billingItems ? `$${num(entry.gross, 3)} / $${num(entry.covered, 3)} ${TAG.billing}` : 'n/a',
  ]);
out.push(table(['OS', 'Raw min', 'Multiplier', 'Billed min', 'Gross / covered by included'], osRows.length ? osRows : [['(no usage)', '', '', '', '']]));

out.push('');
out.push(`Breakdown by repository - private (counts against included minutes)`);
/** @type {string[][]} */
const privateRows = [];
/** @type {{ repo: string, raw: number, weighted: number, runs: number, timingRaw: number, workflows: { name: string, path: string, raw: number, weighted: number, jobs: { name: string, raw: number, weighted: number, attempts: number }[] }[] }[]} */
const privateBreakdown = [];
const privateSorted = privateRepos
  .map((repo) => {
    const perSku = billingRepoSku.get(repo.name) ?? new Map();
    let raw = 0;
    let weighted = 0;
    for (const [sku, quantity] of perSku) {
      raw += quantity;
      weighted += quantity * costWeight(quotas, sku);
    }
    return { repo, raw, weighted };
  })
  .filter((entry) => entry.raw > 0 || runCountByRepo.has(entry.repo.nameWithOwner))
  .sort((a, b) => b.weighted - a.weighted);
for (const { repo, raw, weighted } of privateSorted) {
  const jobs = jobsByRepo.get(repo.nameWithOwner) ?? [];
  const timingRaw = jobs.reduce((sum, job) => sum + job.rawMinutes, 0);
  privateRows.push([repo.nameWithOwner, `${minutes(raw)} ${minutesSource}`, `${minutes(weighted)} ${TAG.estimate}`, `${num(runCountByRepo.get(repo.nameWithOwner) ?? 0)} runs ${TAG.timing}`]);
  /** @type {Map<string, Map<string, { raw: number, weighted: number, attempts: number }>>} */
  const byWorkflow = new Map();
  /** @type {Map<string, { name: string, path: string }>} */
  const workflowInfo = new Map();
  for (const job of jobs) {
    const workflowKey = `${job.workflowName} (${job.workflowPath})`;
    workflowInfo.set(workflowKey, { name: job.workflowName, path: job.workflowPath });
    const perJob = byWorkflow.get(workflowKey) ?? new Map();
    const entry = perJob.get(job.jobName) ?? { raw: 0, weighted: 0, attempts: 0 };
    entry.raw += job.rawMinutes;
    entry.weighted += job.weightedMinutes;
    entry.attempts += 1;
    perJob.set(job.jobName, entry);
    byWorkflow.set(workflowKey, perJob);
  }
  const repoEntry = { repo: repo.nameWithOwner, raw, weighted, runs: runCountByRepo.get(repo.nameWithOwner) ?? 0, timingRaw, workflows: /** @type {any[]} */ ([]) };
  privateBreakdown.push(repoEntry);
  for (const [workflow, perJob] of byWorkflow) {
    const workflowRaw = [...perJob.values()].reduce((sum, entry) => sum + entry.raw, 0);
    const workflowWeighted = [...perJob.values()].reduce((sum, entry) => sum + entry.weighted, 0);
    repoEntry.workflows.push({
      ...workflowInfo.get(workflow),
      raw: workflowRaw,
      weighted: workflowWeighted,
      jobs: [...perJob.entries()].sort((a, b) => b[1].weighted - a[1].weighted).map(([name, entry]) => ({ name, ...entry })),
    });
    privateRows.push([`  ${workflow}`, `${minutes(workflowRaw)} ${TAG.timing}`, `${minutes(workflowWeighted)} ${TAG.estimate}`, '']);
    for (const [jobName, entry] of [...perJob.entries()].sort((a, b) => b[1].weighted - a[1].weighted)) {
      privateRows.push([`    ${jobName}`, `${minutes(entry.raw)} ${TAG.timing}`, `${minutes(entry.weighted)} ${TAG.estimate}`, `${num(entry.attempts)} job runs ${TAG.timing}`]);
    }
  }
  if (billingItems && timingRaw !== raw) {
    privateRows.push([`  note: run timing sums to ${minutes(timingRaw)} ${TAG.timing}, billing API ${minutes(raw)} ${TAG.billing}`, '', '', '']);
  }
}
out.push(table(['Repository / workflow / job', 'Raw min', 'Billed min', 'Runs'], privateRows.length ? privateRows : [['(no private-repo usage this cycle)', '', '', '']]));

out.push('');
out.push('Public repositories - free on standard GitHub-hosted runners (docs: billing page, "What is free")');
const publicBreakdown = publicRepos
  .filter((repo) => runCountByRepo.has(repo.nameWithOwner))
  .map((repo) => {
    const jobs = jobsByRepo.get(repo.nameWithOwner) ?? [];
    return {
      repo: repo.nameWithOwner,
      runs: runCountByRepo.get(repo.nameWithOwner) ?? 0,
      raw: jobs.reduce((sum, job) => sum + job.rawMinutes, 0),
      largerRunner: jobs.some((job) => job.sku in quotas.largerRunnerRates),
    };
  });
const publicRows = publicBreakdown.map((entry) => [
  entry.repo,
  `${num(entry.runs)} runs ${TAG.timing}`,
  `${minutes(entry.raw)} ${TAG.timing}`,
  entry.largerRunner ? 'larger runner used: billed' : 'free',
]);
out.push(table(['Repository', 'Runs', 'Raw min', 'Billing'], publicRows.length ? publicRows : [['(no public-repo runs this cycle)', '', '', '']]));
const idlePublic = publicRepos.length - publicRows.length;
const idlePrivate = privateRepos.length - privateSorted.length;
out.push(`(${num(idlePrivate)} private and ${num(idlePublic)} public repositories had no runs this cycle.)`);

console.log(out.join('\n'));

if (args.json) {
  const limit = (
    /** @type {string} */ key,
    /** @type {string} */ label,
    /** @type {string} */ unit,
    /** @type {number} */ used,
    /** @type {string} */ usedTag,
    /** @type {number} */ included,
    /** @type {string} */ includedNote,
    /** @type {number} */ projected,
  ) => {
    const usedPct = percentOf(used, included);
    const projectedPct = percentOf(projected, included);
    return { key, label, unit, used, usedTag, included, includedNote, usedPct, projected, projectedTag: TAG.estimate, projectedPct, status: statusFor(usedPct, projectedPct, thresholds) };
  };
  const report = {
    kind: 'usage',
    account: login,
    plan: planName,
    generatedAt: now.toISOString(),
    cycle: { start: cycle.start.toISOString(), end: cycle.end.toISOString(), daysInCycle: cycle.daysInCycle, daysElapsed: cycle.daysElapsed, daysLeft: cycle.daysLeft },
    thresholds,
    minElapsedDays: minElapsed,
    notices,
    minutesSource,
    limits: [
      limit('minutes-weighted', 'Minutes (cost-weighted)', 'min', weightedTotal, TAG.estimate, includedMinutes, '', weightedProjected),
      limit('minutes-raw', 'Minutes (raw)', 'min', rawTotal, minutesSource, includedMinutes, '', rawProjected),
      limit('artifacts', 'Artifact storage', 'GB', artifactUsedGB, artifactSource, plan.artifactStorageGB, '', artifactProjectedGB),
      limit('cache', 'Cache (largest repo)', 'GB', largestCache.gb, TAG.estimate, plan.cacheStoragePerRepoGB, `per repo; largest is ${largestCache.repo}`, largestCache.gb),
    ],
    byOs: [...byOs.entries()]
      .sort((a, b) => b[1].weighted - a[1].weighted)
      .map(([os, entry]) => ({ os, raw: entry.raw, weighted: entry.weighted, multiplier: entry.raw > 0 ? entry.weighted / entry.raw : 0, gross: entry.gross, covered: entry.covered, hasDollars: Boolean(billingItems) })),
    daily: [...dailyMinutes.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([date, entry]) => ({ date, ...entry })),
    dailySource: minutesSource,
    storageFromBilling: storageItems.length > 0,
    privateRepos: privateBreakdown,
    publicRepos: publicBreakdown,
    idle: { private: idlePrivate, public: idlePublic },
    cacheByRepo,
  };
  writeFileSync(args.json, JSON.stringify(report, null, 2));
}

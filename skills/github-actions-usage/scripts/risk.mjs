#!/usr/bin/env node
// @ts-check
// Risk review of one repository's GitHub Actions workflows against its last 30 days of runs. Read-only:
// it only proposes YAML changes.
//
// Usage: node risk.mjs --list                    list the user's repos to pick one
//        node risk.mjs <owner/repo> [--local <checkout dir>] [--now <ISO date>] [--json <file>]
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { parseArgs } from 'node:util';
import { bytesToGB, observedDays, scaleTo30Days } from './lib/calc.mjs';
import { GhError, apiGet, apiGetAll, ghJson, missingScopeNotice, tokenScopes } from './lib/gh.mjs';
import { jobRecords, listRuns } from './lib/history.mjs';
import { TAG, gigabytes, minutes, num, table } from './lib/format.mjs';
import { parseYaml } from './lib/yaml.mjs';
import * as rules from './lib/rules.mjs';

const quotas = JSON.parse(readFileSync(new URL('./lib/quotas.json', import.meta.url), 'utf8'));
const config = JSON.parse(readFileSync(new URL('./lib/config.json', import.meta.url), 'utf8'));
const MS_PER_DAY = 86_400_000;

const { values: args, positionals } = parseArgs({
  allowPositionals: true,
  options: { list: { type: 'boolean' }, local: { type: 'string' }, now: { type: 'string' }, owner: { type: 'string' }, json: { type: 'string' } },
});
const now = args.now ? new Date(args.now) : new Date();
const windowStart = new Date(now.getTime() - config.risk.windowDays * MS_PER_DAY);

const scopes = tokenScopes();
if (scopes !== null && !scopes.includes('repo')) {
  console.log(missingScopeNotice('repo', 'Private repositories, their runs and workflow files cannot be read.'));
}

if (args.list || positionals.length === 0) {
  listRepos();
} else {
  review(positionals[0]);
}

function listRepos() {
  const owner = args.owner ?? apiGet('user').login;
  /** @type {{ nameWithOwner: string, visibility: string, isArchived: boolean, pushedAt: string }[]} */
  const repos = ghJson(['repo', 'list', owner, '--limit', '1000', '--json', 'nameWithOwner,visibility,isArchived,pushedAt']);
  const created = encodeURIComponent(`>=${windowStart.toISOString().replace(/\.\d{3}Z$/, 'Z')}`);
  const rows = repos
    .filter((repo) => !repo.isArchived)
    .map((repo) => {
      const runs = apiGet(`repos/${repo.nameWithOwner}/actions/runs?per_page=1&created=${created}`).total_count;
      return { repo, runs };
    })
    .sort((a, b) => b.runs - a.runs || a.repo.nameWithOwner.localeCompare(b.repo.nameWithOwner));
  console.log(`Repositories for ${owner} - runs in the last ${config.risk.windowDays} days ${TAG.timing}`);
  console.log(
    table(
      ['#', 'Repository', 'Visibility', 'Runs (30 d)', 'Last push'],
      rows.map(({ repo, runs }, index) => [
        String(index + 1),
        repo.nameWithOwner,
        repo.visibility.toLowerCase() === 'public' ? 'public (free on standard runners)' : repo.visibility.toLowerCase(),
        `${num(runs)} runs ${TAG.timing}`,
        repo.pushedAt.slice(0, 10),
      ]),
    ),
  );
  console.log('\nPick one and run: node risk.mjs <owner/repo>');
}

/**
 * Workflow files to review: every .yml/.yaml under .github/workflows on any branch (deduplicated by
 * blob). For a path with several versions, prefer the branch of its most recent run, then the default branch.
 * @param {string} repo
 * @param {string} defaultBranch
 * @param {import('./lib/history.mjs').Run[]} runs
 * @returns {{ workflows: rules.Workflow[], notes: string[] }}
 */
function loadWorkflows(repo, defaultBranch, runs) {
  /** @type {string[]} */
  const branches = apiGetAll(`repos/${repo}/branches?per_page=100`, '').map((branch) => branch.name);
  const ordered = [defaultBranch, ...branches.filter((name) => name !== defaultBranch)];
  /** @type {Map<string, Map<string, string[]>>} path -> blob sha -> branches */
  const versions = new Map();
  for (const branch of ordered) {
    let listing;
    try {
      listing = apiGet(`repos/${repo}/contents/.github/workflows?ref=${encodeURIComponent(branch)}`);
    } catch (error) {
      if (error instanceof GhError && error.isNotFound) continue;
      throw error;
    }
    for (const entry of listing) {
      if (entry.type !== 'file' || !/\.ya?ml$/.test(entry.name)) continue;
      const byBlob = versions.get(entry.path) ?? new Map();
      byBlob.set(entry.sha, [...(byBlob.get(entry.sha) ?? []), branch]);
      versions.set(entry.path, byBlob);
    }
  }
  /** @type {rules.Workflow[]} */
  const workflows = [];
  const notes = [];
  for (const [path, byBlob] of versions) {
    const latestRun = runs.filter((run) => run.path === path).sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0];
    const allBranches = [...byBlob.values()].flat();
    const ref =
      latestRun && allBranches.includes(latestRun.head_branch)
        ? latestRun.head_branch
        : allBranches.includes(defaultBranch)
          ? defaultBranch
          : allBranches[0];
    const file = apiGet(`repos/${repo}/contents/${path}?ref=${encodeURIComponent(ref)}`);
    const source = Buffer.from(file.content, 'base64').toString('utf8');
    workflows.push({ path, ref, doc: parseYaml(source) });
    if (!allBranches.includes(defaultBranch)) notes.push(`${path} is not on the default branch (${defaultBranch}); reviewed @ ${ref}.`);
    if (byBlob.size > 1) notes.push(`${path} differs across branches (${byBlob.size} versions); reviewed @ ${ref}.`);
    if (args.local) {
      const localPath = join(args.local, path);
      if (!existsSync(localPath)) notes.push(`${path} is not in the local checkout ${args.local}.`);
      else if (readFileSync(localPath, 'utf8') !== source) notes.push(`${path} in the local checkout differs from ${ref} on GitHub (unpushed changes are not reviewed).`);
    }
  }
  if (args.local) {
    const localDir = join(args.local, '.github/workflows');
    if (existsSync(localDir)) {
      for (const name of readdirSync(localDir).filter((entry) => /\.ya?ml$/.test(entry))) {
        if (!versions.has(`.github/workflows/${name}`)) notes.push(`.github/workflows/${name} exists only in the local checkout.`);
      }
    }
  }
  return { workflows, notes };
}

/**
 * @param {string} repo owner/name
 */
function review(repo) {
  const meta = apiGet(`repos/${repo}`);
  const runs = listRuns(repo, windowStart).filter((run) => Date.parse(run.created_at) < now.getTime());
  if (runs.length > config.risk.maxRunsInspected) {
    console.log(`NOTICE: ${runs.length} runs in the window; inspecting the newest ${config.risk.maxRunsInspected}.`);
    runs.splice(config.risk.maxRunsInspected);
  }
  const jobs = jobRecords(repo, runs, quotas);
  const days = observedDays(now, config.risk.windowDays, new Date(meta.created_at));
  const scale = (/** @type {number} */ count) => scaleTo30Days(count, days, config.risk.minObservedDays);
  const { workflows, notes } = loadWorkflows(repo, meta.default_branch, runs);

  /** @type {Map<string, string[] | null>} */
  const filesByRange = new Map();
  const cacheUsage = apiGet(`repos/${repo}/actions/cache/usage`);
  /**
   * Files a run's change touched: for a push, since the previous run's commit on the same branch
   * (first push on a branch: vs the default branch); for a pull_request, vs the default branch.
   * @param {import('./lib/history.mjs').Run} run
   * @returns {string[] | null}
   */
  const changedFiles = (run) => {
    const previous =
      run.event === 'push'
        ? runs
            .filter((other) => other.event === 'push' && other.head_branch === run.head_branch && Date.parse(other.created_at) < Date.parse(run.created_at))
            .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0]
        : undefined;
    const base = previous ? previous.head_sha : meta.default_branch;
    const range = `${base}...${run.head_sha}`;
    if (!filesByRange.has(range)) {
      try {
        const comparison = apiGet(`repos/${repo}/compare/${encodeURIComponent(base)}...${run.head_sha}`);
        filesByRange.set(range, (comparison.files ?? []).map((/** @type {{ filename: string }} */ file) => file.filename));
      } catch (error) {
        if (!(error instanceof GhError)) throw error;
        filesByRange.set(range, null);
      }
    }
    return filesByRange.get(range) ?? null;
  };
  /** @type {rules.Context} */
  const context = {
    quotas,
    config,
    defaultBranch: meta.default_branch,
    scale,
    changedFiles,
    artifacts: apiGetAll(`repos/${repo}/actions/artifacts?per_page=100`, 'artifacts'),
    cache: {
      gb: bytesToGB(cacheUsage.active_caches_size_in_bytes, quotas.storage.bytesPerGB),
      count: cacheUsage.active_caches_count,
      entries: apiGetAll(`repos/${repo}/actions/caches?per_page=100`, 'actions_caches'),
    },
    // Every plan includes the same per-repo cache allowance; use the account's plan when readable.
    cacheLimitGB: (quotas.plans[apiGet('user').plan?.name] ?? quotas.plans.free).cacheStoragePerRepoGB,
  };

  /** @type {rules.Finding[]} */
  const findings = [];
  /** @type {rules.Note[]} */
  const latent = [];
  /** @type {rules.Note[]} */
  const ok = [];
  const collect = (/** @type {rules.RuleResult} */ result) => {
    findings.push(...result.findings);
    latent.push(...result.latent);
    ok.push(...result.ok);
  };
  for (const workflow of workflows) {
    const workflowRuns = runs.filter((run) => run.path === workflow.path);
    const workflowJobs = jobs.filter((job) => job.workflowPath === workflow.path);
    collect(rules.doubleTrigger(workflow, workflowRuns, workflowJobs, context));
    collect(rules.expensiveRunners(workflow, workflowRuns, workflowJobs, context));
    collect(rules.matrixFanOut(workflow, workflowRuns, workflowJobs, context));
    collect(rules.missingPathsFilter(workflow, workflowRuns, workflowJobs, context));
    collect(rules.missingConcurrency(workflow, workflowRuns, workflowJobs, context));
    collect(rules.timeoutsAndRetries(workflow, workflowRuns, workflowJobs, context));
    collect(rules.schedules(workflow, workflowRuns, workflowJobs, context, now));
    collect(rules.artifactRetention(workflow, workflowRuns, context, days));
  }
  collect(rules.cacheGrowth(repo, context));
  findings.sort((a, b) => (a.kind === b.kind ? b.saving - a.saving : a.kind === 'minutes' ? -1 : 1));

  const weightedTotal = jobs.reduce((sum, job) => sum + job.weightedMinutes, 0);
  const rawTotal = jobs.reduce((sum, job) => sum + job.rawMinutes, 0);
  const out = [];
  const isPublic = meta.visibility === 'public';
  out.push(
    `Risk findings - ${repo} (last ${config.risk.windowDays} days: ${num(runs.length)} runs ${TAG.timing}, ${num(jobs.length)} job runs, ` +
      `${minutes(rawTotal)} raw ${TAG.timing} = ${minutes(weightedTotal)} cost-weighted ${TAG.estimate})`,
  );
  out.push(
    `History covers ${num(days, 1)} days${days < config.risk.windowDays ? ` (repo created ${meta.created_at.slice(0, 10)})` : ''}; ` +
      `monthly figures scale that to 30 days. Cost-weighted = raw min x runner rate / Linux 2-core rate.`,
  );
  if (isPublic) out.push('Public repository: standard runners are free here; only larger runners would be billed. Minutes below show what the same workflows would cost in a private repo.');
  out.push(`Workflows reviewed: ${workflows.map((workflow) => `${workflow.path} @ ${workflow.ref}`).join(', ') || 'none found'}`);
  for (const note of notes) out.push(`Note: ${note}`);
  out.push('');

  if (findings.length === 0) out.push('No findings with observed cost in the window.');
  findings.forEach((finding, index) => {
    const isStorage = finding.kind === 'storage';
    const unit = (/** @type {number} */ value) => (isStorage ? gigabytes(value) : minutes(value));
    const perRun =
      finding.perRun === null
        ? 'n/a per run'
        : `${unit(finding.perRun)}/${isStorage ? 'upload' : 'run'} ${finding.perRunSource === 'timing' ? TAG.timing : TAG.estimate}`;
    out.push(`${index + 1}. ${finding.title}`);
    out.push(`   ${perRun}, ${unit(finding.monthly)}${isStorage ? ' held' : '/month'} ${TAG.estimate}`);
    out.push(`   Evidence: ${finding.evidence}`);
    out.push(`   Change:`);
    for (const line of finding.change.split('\n')) out.push(`     ${line}`);
    out.push(`   Saving: ~${unit(finding.saving)}${isStorage ? '' : '/month'} ${TAG.estimate}${finding.savingNote ? ` - ${finding.savingNote}` : ''}`);
    out.push('');
  });
  const combined = rules.combinedSaving(findings, jobs, scale);
  if (findings.some((finding) => finding.kind === 'minutes')) {
    const monthlyNow = scale(weightedTotal);
    out.push(
      `Combined, if all minute changes are applied: ~${minutes(combined)}/month saved of ~${minutes(monthlyNow)}/month ${TAG.estimate} ` +
        `(savings overlap, so this is not the sum of the lines above).`,
    );
    out.push('');
  }

  if (latent.length > 0) {
    out.push('Latent risks (no cost observed in the window):');
    for (const note of latent) {
      const [first, ...rest] = note.detail.split('\n');
      out.push(`- ${note.title} - ${note.evidence}`);
      out.push(`  ${first}`);
      for (const line of rest) out.push(`    ${line}`);
    }
    out.push('');
  }
  if (ok.length > 0) {
    out.push('Checked, no action needed:');
    for (const note of ok) out.push(`- ${note.title}: ${note.detail} - ${note.evidence}`);
  }
  console.log(out.join('\n'));

  if (args.json) {
    const report = {
      kind: 'risk',
      repo,
      visibility: meta.visibility,
      defaultBranch: meta.default_branch,
      repoCreatedAt: meta.created_at,
      generatedAt: now.toISOString(),
      windowDays: config.risk.windowDays,
      observedDays: days,
      runs: runs.length,
      jobRuns: jobs.length,
      rawTotal,
      weightedTotal,
      monthlyNow: scale(weightedTotal),
      combinedSaving: findings.some((finding) => finding.kind === 'minutes') ? combined : 0,
      workflows: workflows.map((workflow) => ({ path: workflow.path, ref: workflow.ref })),
      notes,
      findings: findings.map(({ reductions, ...finding }) => finding),
      latent,
      ok,
    };
    writeFileSync(args.json, JSON.stringify(report, null, 2));
  }
}

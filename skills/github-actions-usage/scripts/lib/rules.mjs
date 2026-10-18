// @ts-check
// Risk rules (references/risk-rules.md). Pure: each rule takes a parsed workflow plus its run/job
// history and returns findings. Savings are recorded per job attempt so overlapping findings can be
// combined without double counting.
import {
  costWeight,
  cronRunsIn30Days,
  failFastSavedMinutes,
  matrixSize,
  minutesSavedByCancelling,
  skuForLabels,
} from './calc.mjs';
import { lineOf } from './yaml.mjs';

/**
 * @typedef {import('./history.mjs').JobRecord} JobRecord
 * @typedef {import('./history.mjs').Run} Run
 * @typedef {{ kind: 'minutes' | 'storage', title: string, evidence: string, perRun: number | null,
 *   perRunSource: 'timing' | 'estimate', monthly: number, change: string, saving: number, savingNote?: string,
 *   reductions: Map<string, number> }} Finding
 * @typedef {{ title: string, evidence: string, detail: string }} Note
 * @typedef {{ path: string, ref: string, doc: Record<string, any> }} Workflow
 * @typedef {{ quotas: any, config: any, defaultBranch: string, scale: (count: number) => number,
 *   changedFiles: (run: Run) => string[] | null, artifacts: any[], cache: { gb: number, count: number, entries: any[] },
 *   cacheLimitGB: number }} Context
 * @typedef {{ findings: Finding[], latent: Note[], ok: Note[] }} RuleResult
 */

/**
 * @param {JobRecord} job
 * @returns {string}
 */
export function jobKey(job) {
  return `${job.runId}/${job.runAttempt}/${job.jobName}`;
}

/**
 * Normalize `on:` (string, list or map) to a map of event -> config object.
 * @param {unknown} on
 * @returns {Record<string, any>}
 */
export function triggers(on) {
  if (typeof on === 'string') return { [on]: {} };
  if (Array.isArray(on)) return Object.fromEntries(on.map((event) => [String(event), {}]));
  if (on && typeof on === 'object') {
    return Object.fromEntries(Object.entries(on).map(([event, value]) => [event, value ?? {}]));
  }
  return {};
}

/**
 * The workflow's `on` value. Some YAML readers turn the bare key `on` into `true`; ours keeps it.
 * @param {Record<string, any>} doc
 * @returns {unknown}
 */
function onOf(doc) {
  return doc.on ?? doc.true;
}

/**
 * Glob to regex: `**` any path, `*` any run of non-slash characters. A pattern without '/' matches
 * the basename anywhere.
 * @param {string} pattern
 * @returns {RegExp}
 */
export function globToRegExp(pattern) {
  let source = '';
  for (let index = 0; index < pattern.length; index++) {
    const char = pattern[index];
    if (char === '*' && pattern[index + 1] === '*') {
      source += '.*';
      index++;
      if (pattern[index + 1] === '/') index++;
    } else if (char === '*') source += '[^/]*';
    else if (char === '?') source += '[^/]';
    else source += char.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(pattern.includes('/') ? `^${source}$` : `(^|/)${source}$`);
}

/**
 * @param {string[]} files
 * @param {string[]} patterns
 * @returns {boolean}
 */
export function allFilesMatch(files, patterns) {
  const regexes = patterns.map(globToRegExp);
  return files.length > 0 && files.every((file) => regexes.some((regex) => regex.test(file)));
}

/**
 * Regex matching the API job names a workflow job produces: its `name:` with expressions as
 * wildcards, or `<id>` / `<id> (<matrix values>)`, optionally followed by " / <called job>".
 * @param {string} id
 * @param {any} job
 * @returns {RegExp}
 */
export function jobNameMatcher(id, job) {
  const escape = (/** @type {string} */ text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const name = typeof job?.name === 'string' ? job.name : null;
  const body = name
    ? name
        .split(/\$\{\{.*?\}\}/)
        .map(escape)
        .join('.+')
    : `${escape(id)}( \\(.+\\))?`;
  return new RegExp(`^${body}( / .+)?$`);
}

/**
 * @param {Workflow} workflow
 * @param {number | undefined} line
 * @returns {string}
 */
function at(workflow, line) {
  return `${workflow.path}${line ? `:${line}` : ''} (@ ${workflow.ref})`;
}

/**
 * @param {JobRecord[]} jobs
 * @returns {number}
 */
function weightedSum(jobs) {
  return jobs.reduce((sum, job) => sum + job.weightedMinutes, 0);
}

/**
 * @param {JobRecord[]} jobs
 * @returns {Map<string, number>}
 */
function reductionsFor(jobs) {
  return new Map(jobs.map((job) => [jobKey(job), job.weightedMinutes]));
}

/**
 * @param {JobRecord[]} jobs
 * @returns {Map<string, JobRecord[]>}
 */
function groupByRun(jobs) {
  /** @type {Map<string, JobRecord[]>} */
  const groups = new Map();
  for (const job of jobs) {
    const key = `${job.runId}/${job.runAttempt}`;
    groups.set(key, [...(groups.get(key) ?? []), job]);
  }
  return groups;
}

/**
 * Average weighted minutes per run of the workflow.
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @returns {number | null}
 */
function perRun(runs, jobs) {
  return runs.length > 0 ? weightedSum(jobs) / runs.length : null;
}

/**
 * Rule: push and pull_request both fire for one change on a non-default branch.
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @returns {RuleResult}
 */
export function doubleTrigger(workflow, runs, jobs, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const on = triggers(onOf(workflow.doc));
  const onNode = /** @type {any} */ (onOf(workflow.doc));
  const evidence = at(workflow, lineOf(onNode, 'push') ?? lineOf(workflow.doc, 'on'));
  if (!on.push || !on.pull_request) {
    result.ok.push({ title: 'push + pull_request double runs', evidence, detail: 'not both triggered' });
    return result;
  }
  const branches = on.push.branches;
  const pushLimitedToDefault =
    Array.isArray(branches) && branches.every((branch) => branch === context.defaultBranch);
  if (pushLimitedToDefault || on.push.tags && !branches) {
    result.ok.push({ title: 'push + pull_request double runs', evidence, detail: `push limited to ${context.defaultBranch}` });
    return result;
  }
  const prShas = new Set(runs.filter((run) => run.event === 'pull_request').map((run) => run.head_sha));
  const duplicateRunIds = new Set(
    runs.filter((run) => run.event === 'push' && prShas.has(run.head_sha)).map((run) => run.id),
  );
  const duplicateJobs = jobs.filter((job) => duplicateRunIds.has(job.runId));
  const change = [
    'on:',
    '  push:',
    `    branches: [${context.defaultBranch}]`,
    '  pull_request:',
  ].join('\n');
  if (duplicateRunIds.size === 0) {
    result.latent.push({
      title: 'push on every branch plus pull_request',
      evidence,
      detail: `each push to a branch with an open PR runs the workflow twice; no duplicates in the window. Fix:\n${change}`,
    });
    return result;
  }
  const monthly = context.scale(weightedSum(duplicateJobs));
  result.findings.push({
    kind: 'minutes',
    title: `push + pull_request run twice for one change (${duplicateRunIds.size} of ${runs.length} runs were duplicate push runs)`,
    evidence,
    perRun: perRun(runs, jobs),
    perRunSource: 'timing',
    monthly,
    change,
    saving: monthly,
    reductions: reductionsFor(duplicateJobs),
  });
  return result;
}

/**
 * Rule: jobs on runners that cost more than Linux 2-core (macOS, Windows, larger runners).
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @returns {RuleResult}
 */
export function expensiveRunners(workflow, runs, jobs, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const definitions = /** @type {Record<string, any>} */ (workflow.doc.jobs ?? {});
  for (const [id, definition] of Object.entries(definitions)) {
    const matcher = jobNameMatcher(id, definition);
    const matched = jobs.filter((job) => matcher.test(job.jobName));
    const evidence = at(workflow, lineOf(definition, 'runs-on') ?? lineOf(definitions, id));
    const legs = new Map();
    for (const job of matched) legs.set(job.jobName, [...(legs.get(job.jobName) ?? []), job]);

    if (matched.length === 0) {
      const labels = runsOnLabels(definition);
      const sku = skuForLabels(context.quotas, labels);
      const weight = costWeight(context.quotas, sku);
      if (weight > 1) {
        const timeout = definition['timeout-minutes'] ?? context.quotas.workflowDefaults.jobTimeoutMinutes;
        result.latent.push({
          title: `job "${id}" on ${labels.join(', ')} (${weight.toFixed(2)}x)`,
          evidence,
          detail: `no runs in the window; a run can bill up to ${Math.round(timeout * weight)} cost-weighted min (timeout ${timeout} min x ${weight.toFixed(2)}).`,
        });
      }
      continue;
    }

    const isMatrix = Boolean(definition.strategy?.matrix);
    for (const [legName, legJobs] of legs) {
      const weight = legJobs[0].weight;
      if (weight <= 1) continue;
      const raw = legJobs.reduce((sum, job) => sum + job.rawMinutes, 0);
      const weighted = weightedSum(legJobs);
      const gatedJobs = legJobs.filter((job) => job.event !== 'pull_request');
      const switchSaving = context.scale(weighted - raw);
      const label = legJobs[0].labels.join(', ');
      const matrixKey = String(definition['runs-on'] ?? '').match(/matrix\.(\w+)/)?.[1] ?? 'os';
      const change = isMatrix
        ? [
            `strategy:`,
            `  matrix:`,
            `    # ${label} only on pull requests`,
            `    ${matrixKey}: \${{ github.event_name == 'pull_request' && fromJSON('${JSON.stringify(definition.strategy.matrix[matrixKey] ?? [label])}') || fromJSON('["ubuntu-latest"]') }}`,
          ].join('\n')
        : `if: github.event_name == 'pull_request'   # run ${label} only on pull requests`;
      const gatedSaving = context.scale(weightedSum(gatedJobs));
      result.findings.push({
        kind: 'minutes',
        title: `${legName} runs on ${label} at ${weight.toFixed(2)}x the Linux rate`,
        evidence,
        perRun: weighted / legJobs.length,
        perRunSource: 'timing',
        monthly: context.scale(weighted),
        change,
        saving: gatedSaving,
        savingNote:
          `${gatedJobs.length} of ${legJobs.length} job runs were not pull_request events. ` +
          `Alternative if the job does not need ${label}: runs-on: ubuntu-latest saves ~${Math.round(switchSaving)} min/month.`,
        reductions: reductionsFor(gatedJobs),
      });
    }
  }
  return result;
}

/**
 * @param {any} definition
 * @returns {string[]}
 */
function runsOnLabels(definition) {
  const runsOn = definition?.['runs-on'];
  if (typeof runsOn === 'string') {
    const matrixReference = runsOn.match(/\$\{\{\s*matrix\.(\w+)\s*\}\}/);
    if (matrixReference) {
      const values = definition.strategy?.matrix?.[matrixReference[1]];
      return Array.isArray(values) ? values.map(String) : [runsOn];
    }
    return [runsOn];
  }
  if (Array.isArray(runsOn)) return runsOn.map(String);
  if (runsOn && typeof runsOn === 'object' && Array.isArray(runsOn.labels)) return runsOn.labels.map(String);
  return [];
}

/**
 * Rule: matrix fan-out, and fail-fast: false letting sibling legs run on after a failure.
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @returns {RuleResult}
 */
export function matrixFanOut(workflow, runs, jobs, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const definitions = /** @type {Record<string, any>} */ (workflow.doc.jobs ?? {});
  for (const [id, definition] of Object.entries(definitions)) {
    const matrix = definition.strategy?.matrix;
    if (!matrix || typeof matrix !== 'object') continue;
    const size = matrixSize(matrix);
    const evidence = at(workflow, lineOf(definition.strategy, 'matrix'));
    const matcher = jobNameMatcher(id, definition);
    const matched = jobs.filter((job) => matcher.test(job.jobName));
    const failFast = definition.strategy['fail-fast'] !== false;
    /** @type {Map<string, number>} */
    const reductions = new Map();
    let saved = 0;
    if (!failFast) {
      for (const legs of groupByRun(matched).values()) {
        const failures = legs.filter((leg) => leg.conclusion === 'failure').map((leg) => Date.parse(leg.completedAt));
        if (failures.length === 0) continue;
        saved += failFastSavedMinutes(legs);
        const firstFailure = Math.min(...failures);
        for (const leg of legs) {
          const legSaving = minutesSavedByCancelling(leg, firstFailure);
          if (legSaving > 0) reductions.set(jobKey(leg), legSaving);
        }
      }
    }
    const perRunMinutes = matched.length > 0 ? weightedSum(matched) / groupByRun(matched).size : null;
    if (saved > 0) {
      const monthly = context.scale(saved);
      result.findings.push({
        kind: 'minutes',
        title: `matrix "${id}" fans out to ${size} jobs per run with fail-fast: false; legs keep running after another leg fails`,
        evidence: at(workflow, lineOf(definition.strategy, 'fail-fast') ?? lineOf(definition.strategy, 'matrix')),
        perRun: perRunMinutes,
        perRunSource: 'timing',
        monthly: context.scale(weightedSum(matched)),
        change: 'strategy:\n  fail-fast: true',
        saving: monthly,
        savingNote: 'only failing runs save; you lose the other legs\' results on a failure.',
        reductions,
      });
    } else {
      result.latent.push({
        title: `matrix "${id}" fans out to ${size} jobs per run`,
        evidence,
        detail:
          `${perRunMinutes === null ? 'no runs in the window' : `~${Math.round(perRunMinutes)} cost-weighted min per run`}; ` +
          (failFast ? 'fail-fast is on.' : 'fail-fast: false, but no failure cut a sibling short in the window.'),
      });
    }
  }
  return result;
}

/**
 * Rule: push / pull_request without a paths filter, measured by runs whose changed files were all
 * docs-like (push: files since the previous run on the branch; pull_request: files vs the default branch).
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @returns {RuleResult}
 */
export function missingPathsFilter(workflow, runs, jobs, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const on = triggers(onOf(workflow.doc));
  const onNode = /** @type {any} */ (onOf(workflow.doc));
  const events = ['push', 'pull_request'].filter((event) => on[event]);
  const unfiltered = events.filter((event) => !on[event].paths && !on[event]['paths-ignore'] && !(event === 'push' && on.push.tags && !on.push.branches));
  if (unfiltered.length === 0) {
    if (events.length > 0) {
      const tagOnly = events.every((event) => event === 'push' && on.push.tags && !on.push.branches);
      result.ok.push({ title: 'paths filter', evidence: at(workflow, lineOf(onNode, events[0])), detail: tagOnly ? 'not needed: push runs on tags only' : 'present' });
    }
    return result;
  }
  const patterns = context.config.risk.docsOnlyPatterns;
  const candidates = runs.filter((run) => unfiltered.includes(run.event));
  const docsOnlyRunIds = new Set();
  let inspected = 0;
  for (const run of candidates) {
    const files = context.changedFiles(run);
    if (files === null) continue;
    inspected++;
    if (allFilesMatch(files, patterns)) docsOnlyRunIds.add(run.id);
  }
  const change = [`on:`, ...unfiltered.flatMap((event) => [`  ${event}:`, `    paths-ignore: [${patterns.map((pattern) => `'${pattern}'`).join(', ')}]`])].join('\n');
  const evidence = at(workflow, lineOf(onNode, unfiltered[0]));
  if (docsOnlyRunIds.size === 0) {
    result.latent.push({
      title: `no paths filter on ${unfiltered.join(' / ')}`,
      evidence,
      detail: `0 of ${inspected} runs changed only docs-like files; every change, including docs, runs the full workflow.`,
    });
    return result;
  }
  const avoided = jobs.filter((job) => docsOnlyRunIds.has(job.runId));
  const monthly = context.scale(weightedSum(avoided));
  result.findings.push({
    kind: 'minutes',
    title: `no paths filter: ${docsOnlyRunIds.size} of ${inspected} runs changed only docs-like files`,
    evidence,
    perRun: perRun(runs, jobs),
    perRunSource: 'timing',
    monthly,
    change,
    saving: monthly,
    savingNote: 'push: files since the previous run on the branch; pull_request: files vs the default branch.',
    reductions: reductionsFor(avoided),
  });
  return result;
}

/**
 * Rule: no concurrency cancel, measured by runs superseded by a newer run on the same branch and event.
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @returns {RuleResult}
 */
export function missingConcurrency(workflow, runs, jobs, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const concurrency = workflow.doc.concurrency;
  const cancels = concurrency && typeof concurrency === 'object' && concurrency['cancel-in-progress'] === true;
  if (cancels) {
    const on = triggers(onOf(workflow.doc));
    const group = String(concurrency.group ?? '');
    const crossEvent = on.push && on.pull_request && /github\.ref\b/.test(group);
    result.ok.push({
      title: 'concurrency cancel-in-progress',
      evidence: at(workflow, lineOf(workflow.doc, 'concurrency')),
      detail: `group "${group}"${crossEvent ? '; push and pull_request have different refs, so this does not dedupe a push against its PR run' : ''}`,
    });
    return result;
  }
  /** @type {Map<string, number>} */
  const reductions = new Map();
  let superseded = 0;
  const ordered = [...runs].sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at));
  for (const [index, run] of ordered.entries()) {
    const newer = ordered
      .slice(index + 1)
      .find((other) => other.head_branch === run.head_branch && other.event === run.event && Date.parse(other.created_at) < Date.parse(run.updated_at));
    if (!newer) continue;
    const cancelAt = Date.parse(newer.created_at);
    let runSaving = 0;
    for (const job of jobs.filter((record) => record.runId === run.id)) {
      const saving = minutesSavedByCancelling(job, cancelAt);
      if (saving > 0) reductions.set(jobKey(job), saving);
      runSaving += saving;
    }
    if (runSaving > 0) superseded++;
  }
  const change = ['concurrency:', '  group: ${{ github.workflow }}-${{ github.ref }}', '  cancel-in-progress: true'].join('\n');
  const evidence = at(workflow, lineOf(workflow.doc, 'on'));
  const savedMinutes = [...reductions.values()].reduce((sum, value) => sum + value, 0);
  if (savedMinutes === 0) {
    result.latent.push({ title: 'no concurrency cancel-in-progress', evidence, detail: `no superseded runs in the window. Fix:\n${change}` });
    return result;
  }
  const monthly = context.scale(savedMinutes);
  result.findings.push({
    kind: 'minutes',
    title: `no concurrency cancel: ${superseded} runs kept going after a newer run started on the same branch`,
    evidence,
    perRun: perRun(runs, jobs),
    perRunSource: 'timing',
    monthly,
    change,
    saving: monthly,
    reductions,
  });
  return result;
}

/**
 * Rule: missing or loose timeout-minutes (exposure if a job hangs), and re-run attempts.
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @returns {RuleResult}
 */
export function timeoutsAndRetries(workflow, runs, jobs, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const { timeoutSlackFactor, minSuggestedTimeoutMinutes } = context.config.risk;
  const defaultTimeout = context.quotas.workflowDefaults.jobTimeoutMinutes;
  const definitions = /** @type {Record<string, any>} */ (workflow.doc.jobs ?? {});
  for (const [id, definition] of Object.entries(definitions)) {
    const matcher = jobNameMatcher(id, definition);
    const matched = jobs.filter((job) => matcher.test(job.jobName));
    const explicit = definition['timeout-minutes'];
    const timeout = typeof explicit === 'number' ? explicit : defaultTimeout;
    const evidence = at(workflow, lineOf(definition, 'timeout-minutes') ?? lineOf(definitions, id));
    const labels = matched.length > 0 ? matched[0].labels : runsOnLabels(definition);
    const weight = Math.max(...labels.map((label) => costWeight(context.quotas, skuForLabels(context.quotas, [label]))), 1);
    if (matched.length === 0) {
      if (typeof explicit !== 'number') {
        result.latent.push({ title: `job "${id}" has no timeout-minutes`, evidence, detail: `defaults to ${defaultTimeout} min; a hung run bills up to ${Math.round(defaultTimeout * weight)} cost-weighted min.` });
      }
      continue;
    }
    const maxObserved = Math.max(...matched.map((job) => job.rawMinutes));
    const suggested = Math.max(Math.ceil(maxObserved * 2), minSuggestedTimeoutMinutes);
    const timedOut = matched.filter((job) => job.conclusion === 'timed_out' || job.conclusion === 'cancelled' && job.rawMinutes >= timeout);
    const loose = timeout > timeoutSlackFactor * maxObserved && timeout > suggested;
    if (timedOut.length > 0 && loose) {
      const savedPerHang = (timeout - suggested) * weight;
      const monthly = context.scale(savedPerHang * timedOut.length);
      result.findings.push({
        kind: 'minutes',
        title: `job "${id}" hit its ${timeout} min timeout ${timedOut.length} times; longest normal run ${maxObserved} min`,
        evidence,
        perRun: timeout * weight,
        perRunSource: 'estimate',
        monthly,
        change: `timeout-minutes: ${suggested}`,
        saving: monthly,
        reductions: new Map(timedOut.map((job) => [jobKey(job), savedPerHang])),
      });
    } else if (loose) {
      result.latent.push({
        title: `job "${id}" timeout ${timeout} min${typeof explicit === 'number' ? '' : ' (default)'} vs longest run ${maxObserved} min`,
        evidence,
        detail: `a hung run bills up to ${Math.round(timeout * weight)} cost-weighted min; timeout-minutes: ${suggested} caps it at ${Math.round(suggested * weight)}.`,
      });
    } else {
      result.ok.push({ title: `timeout for "${id}"`, evidence, detail: `${timeout} min vs longest run ${maxObserved} min` });
    }

    const retrySteps = (Array.isArray(definition.steps) ? definition.steps : []).filter((step) => /retry/i.test(String(step?.uses ?? '')));
    if (retrySteps.length > 0) {
      result.latent.push({ title: `job "${id}" uses a retry action`, evidence, detail: `each retry re-bills the step: ${retrySteps.map((step) => step.uses).join(', ')}` });
    }
  }

  const reruns = jobs.filter((job) => job.runAttempt > 1);
  if (reruns.length > 0) {
    const monthly = context.scale(weightedSum(reruns));
    result.findings.push({
      kind: 'minutes',
      title: `${new Set(reruns.map((job) => job.runId)).size} runs were re-run (${reruns.length} extra job attempts)`,
      evidence: at(workflow, undefined),
      perRun: weightedSum(reruns) / new Set(reruns.map((job) => `${job.runId}/${job.runAttempt}`)).size,
      perRunSource: 'timing',
      monthly,
      change: 'Re-run failed jobs only (gh run rerun <id> --failed), and fix the flaky step that needed the re-run.',
      saving: monthly,
      savingNote: 'upper bound: assumes no re-runs at all.',
      reductions: reductionsFor(reruns),
    });
  }
  return result;
}

/**
 * Rule: scheduled workflows running more often than daily.
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {JobRecord[]} jobs
 * @param {Context} context
 * @param {Date} now
 * @returns {RuleResult}
 */
export function schedules(workflow, runs, jobs, context, now) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const onNode = /** @type {any} */ (onOf(workflow.doc));
  const on = triggers(onNode);
  const entries = Array.isArray(on.schedule) ? on.schedule : [];
  if (entries.length === 0) return result;
  const evidence = at(workflow, lineOf(onNode, 'schedule'));
  const scheduledRuns = runs.filter((run) => run.event === 'schedule');
  const scheduledJobs = jobs.filter((job) => job.event === 'schedule');
  const minutesPerRun = scheduledRuns.length > 0 ? weightedSum(scheduledJobs) / scheduledRuns.length : perRun(runs, jobs);
  const runsPerMonth = entries.reduce((sum, entry) => sum + cronRunsIn30Days(String(entry?.cron ?? ''), now), 0);
  const maxRuns = 30 * context.config.risk.maxDailyScheduleRuns;
  const crons = entries.map((entry) => entry?.cron).join(', ');
  if (minutesPerRun === null) {
    result.latent.push({ title: `schedule ${crons}`, evidence, detail: `${runsPerMonth} runs per 30 days; no runs in the window to measure minutes per run.` });
    return result;
  }
  const monthly = runsPerMonth * minutesPerRun;
  if (runsPerMonth <= maxRuns) {
    result.ok.push({ title: `schedule ${crons}`, evidence, detail: `${runsPerMonth} runs per 30 days, ~${Math.round(monthly)} cost-weighted min/month` });
    return result;
  }
  const saving = (runsPerMonth - maxRuns) * minutesPerRun;
  result.findings.push({
    kind: 'minutes',
    title: `schedule ${crons} fires ${runsPerMonth} times per 30 days`,
    evidence,
    perRun: minutesPerRun,
    perRunSource: scheduledRuns.length > 0 ? 'timing' : 'estimate',
    monthly,
    change: "schedule:\n  - cron: '0 6 * * *'   # daily",
    saving,
    reductions: new Map(),
  });
  return result;
}

/**
 * Rule: artifacts uploaded without retention-days (default 90 days), and cache size/duplication.
 * Storage findings are in GB, not minutes.
 * @param {Workflow} workflow
 * @param {Run[]} runs
 * @param {Context} context
 * @param {number} observedDays
 * @returns {RuleResult}
 */
export function artifactRetention(workflow, runs, context, observedDays) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const definitions = /** @type {Record<string, any>} */ (workflow.doc.jobs ?? {});
  const runIds = new Set(runs.map((run) => run.id));
  const artifacts = context.artifacts.filter((artifact) => runIds.has(artifact.workflow_run?.id));
  const bytesPerGB = context.quotas.storage.bytesPerGB;
  for (const [id, definition] of Object.entries(definitions)) {
    const steps = Array.isArray(definition.steps) ? definition.steps : [];
    for (const step of steps) {
      if (!/actions\/upload-artifact/.test(String(step?.uses ?? ''))) continue;
      const evidence = at(workflow, lineOf(step, 'uses'));
      const retention = Number(step.with?.['retention-days'] ?? context.quotas.storage.artifactRetentionDefaultDays);
      const suggested = context.config.risk.suggestedArtifactRetentionDays;
      if (retention <= suggested) {
        result.ok.push({ title: `artifact retention in "${id}"`, evidence, detail: `${retention} days` });
        continue;
      }
      if (artifacts.length === 0) {
        result.latent.push({ title: `artifact upload in "${id}" keeps ${retention} days`, evidence, detail: `no artifacts in the window. Fix: with: retention-days: ${suggested}` });
        continue;
      }
      const averageGB = artifacts.reduce((sum, artifact) => sum + artifact.size_in_bytes, 0) / artifacts.length / bytesPerGB;
      const uploadsPerDay = artifacts.length / Math.max(observedDays, context.config.risk.minObservedDays);
      const steadyGB = averageGB * uploadsPerDay * retention;
      const suggestedGB = averageGB * uploadsPerDay * suggested;
      result.findings.push({
        kind: 'storage',
        title: `artifacts from "${id}" kept ${retention} days (${artifacts.length} uploads, avg ${(averageGB * 1024).toFixed(1)} MB)`,
        evidence,
        perRun: averageGB,
        perRunSource: 'timing',
        monthly: steadyGB,
        change: `- uses: ${step.uses}\n  with:\n    retention-days: ${suggested}`,
        saving: steadyGB - suggestedGB,
        savingNote: 'GB held at steady state, not minutes.',
        reductions: new Map(),
      });
    }
  }
  return result;
}

/**
 * Repository-level cache check (not per workflow).
 * @param {string} repo
 * @param {Context} context
 * @returns {RuleResult}
 */
export function cacheGrowth(repo, context) {
  /** @type {RuleResult} */
  const result = { findings: [], latent: [], ok: [] };
  const limitGB = context.cacheLimitGB;
  const pct = (context.cache.gb / limitGB) * 100;
  const bytesPerGB = context.quotas.storage.bytesPerGB;
  const keyCounts = new Map();
  for (const entry of context.cache.entries) keyCounts.set(entry.key, (keyCounts.get(entry.key) ?? 0) + 1);
  const duplicated = [...keyCounts.values()].filter((count) => count > 1).length;
  const prBytes = context.cache.entries.filter((entry) => String(entry.ref).startsWith('refs/pull/')).reduce((sum, entry) => sum + entry.size_in_bytes, 0);
  const detail =
    `${context.cache.gb.toFixed(2)} GB in ${context.cache.count} entries (${pct.toFixed(1)}% of ${limitGB} GB); ` +
    `${duplicated} keys stored on more than one ref; ${(prBytes / bytesPerGB).toFixed(2)} GB on refs/pull/*.`;
  if (pct >= context.config.risk.cacheWarnPctOfLimit) {
    result.latent.push({ title: 'cache near the per-repo limit; eviction may thrash', evidence: `${repo} cache`, detail });
  } else {
    result.ok.push({ title: 'cache size', evidence: `${repo} cache`, detail });
  }
  return result;
}

/**
 * Combined monthly saving if every finding's change were applied: per job attempt, the largest
 * reduction any finding claims (capped at what the job billed), summed and scaled.
 * @param {Finding[]} findings
 * @param {JobRecord[]} jobs
 * @param {(count: number) => number} scale
 * @returns {number}
 */
export function combinedSaving(findings, jobs, scale) {
  const billed = new Map(jobs.map((job) => [jobKey(job), job.weightedMinutes]));
  /** @type {Map<string, number>} */
  const best = new Map();
  let unattributed = 0;
  for (const finding of findings) {
    if (finding.kind !== 'minutes') continue;
    if (finding.reductions.size === 0) {
      unattributed += finding.saving;
      continue;
    }
    for (const [key, minutes] of finding.reductions) best.set(key, Math.max(best.get(key) ?? 0, minutes));
  }
  let total = 0;
  for (const [key, minutes] of best) total += Math.min(minutes, billed.get(key) ?? minutes);
  return scale(total) + unattributed;
}

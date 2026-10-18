// @ts-check
// Run and job history for a repository, flattened into one record per job attempt.
import { apiGetAll } from './gh.mjs';
import { costWeight, jobBilledMinutes, skuForLabels } from './calc.mjs';

/**
 * @typedef {{ repo: string, runId: number, event: string, headBranch: string, headSha: string,
 *   workflowPath: string, workflowName: string, runCreatedAt: string, runUpdatedAt: string,
 *   jobName: string, labels: string[], runAttempt: number, startedAt: string, completedAt: string,
 *   conclusion: string | null, rawMinutes: number, sku: string, weight: number, weightedMinutes: number }} JobRecord
 * @typedef {{ id: number, event: string, head_branch: string, head_sha: string, path: string, name: string,
 *   created_at: string, updated_at: string, run_attempt: number, conclusion: string | null }} Run
 */

/**
 * Workflow runs created at or after `since`.
 * @param {string} repo owner/name
 * @param {Date} since
 * @returns {Run[]}
 */
export function listRuns(repo, since) {
  const created = encodeURIComponent(`>=${since.toISOString().replace(/\.\d{3}Z$/, 'Z')}`);
  return apiGetAll(`repos/${repo}/actions/runs?per_page=100&created=${created}`, 'workflow_runs');
}

/**
 * Every job attempt of the given runs (filter=all includes re-run attempts, which are billed too).
 * @param {string} repo
 * @param {Run[]} runs
 * @param {import('./calc.mjs').Quotas} quotas
 * @returns {JobRecord[]}
 */
export function jobRecords(repo, runs, quotas) {
  /** @type {JobRecord[]} */
  const records = [];
  for (const run of runs) {
    const jobs = apiGetAll(`repos/${repo}/actions/runs/${run.id}/jobs?filter=all&per_page=100`, 'jobs');
    for (const job of jobs) {
      const labels = Array.isArray(job.labels) ? job.labels : [];
      const sku = skuForLabels(quotas, labels);
      const rawMinutes = sku === 'self-hosted' ? 0 : jobBilledMinutes(job.started_at, job.completed_at);
      const weight = sku === 'self-hosted' || sku === 'unknown' ? 1 : costWeight(quotas, sku);
      records.push({
        repo,
        runId: run.id,
        event: run.event,
        headBranch: run.head_branch,
        headSha: run.head_sha,
        workflowPath: run.path,
        workflowName: job.workflow_name ?? run.name,
        runCreatedAt: run.created_at,
        runUpdatedAt: run.updated_at,
        jobName: job.name,
        labels,
        runAttempt: job.run_attempt ?? 1,
        startedAt: job.started_at,
        completedAt: job.completed_at,
        conclusion: job.conclusion,
        rawMinutes,
        sku,
        weight,
        weightedMinutes: rawMinutes * weight,
      });
    }
  }
  return records;
}

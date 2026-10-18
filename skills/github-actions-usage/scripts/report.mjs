#!/usr/bin/env node
// @ts-check
// One command for the full report: prints the text report (usage, plus the risk review when --repo is
// given) and writes the same figures as a self-contained HTML file. Read-only; never uploads anything.
//
// Usage: node report.mjs [--repo <owner/repo>] [--local <checkout dir>] [--out <file.html>] [--artifact]
//                        [--user <login>] [--plan <plan>]
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const { values: args } = parseArgs({
  options: {
    repo: { type: 'string' },
    local: { type: 'string' },
    out: { type: 'string' },
    artifact: { type: 'boolean' },
    user: { type: 'string' },
    plan: { type: 'string' },
  },
});

const scriptsDir = fileURLToPath(new URL('.', import.meta.url));
const workDir = mkdtempSync(join(tmpdir(), 'gha-usage-'));
const usageJson = join(workDir, 'usage.json');
const riskJson = join(workDir, 'risk.json');
const today = new Date().toLocaleDateString('en-CA'); // YYYY-MM-DD in local time
const out = args.out ?? join(homedir(), `github-actions-usage-report-${today}.html`);

/**
 * @param {string} script
 * @param {string[]} scriptArgs
 */
function run(script, scriptArgs) {
  execFileSync(process.execPath, [join(scriptsDir, script), ...scriptArgs], { stdio: 'inherit' });
}

try {
  run('usage.mjs', [
    '--json', usageJson,
    ...(args.user ? ['--user', args.user] : []),
    ...(args.plan ? ['--plan', args.plan] : []),
  ]);
  if (args.repo) {
    console.log('\n');
    run('risk.mjs', [args.repo, '--json', riskJson, ...(args.local ? ['--local', args.local] : [])]);
  }
  console.log('');
  run('report-html.mjs', [
    '--usage', usageJson,
    ...(args.repo ? ['--risk', riskJson] : []),
    '--out', out,
    ...(args.artifact ? ['--artifact'] : []),
  ]);
} finally {
  rmSync(workDir, { recursive: true, force: true });
}

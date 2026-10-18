---
name: github-actions-usage
description: Report a GitHub account's GitHub Actions free-tier usage for the current billing cycle (minutes by runner OS, artifact and cache storage, days left, projection to cycle end, per-repo/workflow/job breakdown) and review a repository's workflows for what risks blowing through the included minutes (macOS/Windows runners, matrix fan-out, push+pull_request double runs, missing paths filters or concurrency cancel, long timeouts, re-runs, schedules, artifact retention, cache growth), with concrete YAML changes and estimated monthly savings, as terminal text plus a self-contained HTML report. Use whenever the user asks how many Actions minutes they have left, whether they will run out of GitHub Actions minutes or storage, what is eating their CI minutes, why their GitHub bill or Actions usage is high, or wants a CI cost review of a repo's workflows - even if they just say "check my actions usage" or "am I close to the GitHub limit". Read-only; uses the gh CLI.
---

# GitHub Actions usage

Read-only scripts do all the fetching and arithmetic. Your job is to pick which to run, relay the output
as-is (plain text tables, every number with unit and source tag), and add a short summary on top. Never edit
workflows, cancel runs, delete artifacts or caches, or change settings; only propose changes.

Run from this skill's directory:

1. **Account usage this cycle** (start here for any usage question):
   `node scripts/usage.mjs` (options: `--user <login>`, `--plan free|pro|team|free_org|enterprise`)
2. **Risk review of one repository**:
   - Repo named by the user: `node scripts/risk.mjs <owner/repo>`. If the user has a local checkout of it, add
     `--local <path>` so unpushed workflow changes are flagged.
   - No repo named: run `node scripts/risk.mjs --list`, show the list (repos ordered by runs in the last 30 days,
     public ones marked free), and ask the user which one to review this time. Don't pick for them.

3. **Full report with HTML** (when the user wants a report, a page, something to look at or share, or both
   usage and a review): `node scripts/report.mjs [--repo <owner/repo>] [--local <path>] [--out <file.html>]`.
   It prints the same text as 1 and 2 and writes one self-contained HTML file (default
   `~/github-actions-usage-report-<date>.html`). Give the user the path; they can `open` it.

For "how am I doing and what should I fix", run usage first, then offer the risk review for the repo that
dominates the breakdown.

**Publishing the HTML** is opt-in: do it only when the user asks for a link or to share it, because it sends
their usage data to claude.ai. Then run `report.mjs ... --artifact --out <scratchpad>/github-actions-usage.html`
(page content without a document wrapper) and publish that file with the Artifact tool (`icon: "chart"`, a
one-sentence description naming the account and cycle). The scripts themselves never upload anything.

## Before trusting the numbers

- If a script prints `NOTICE: the gh token is missing the "<scope>" scope`, show the notice and the exact
  `gh auth refresh -h github.com -s <scope>` command to the user. They run interactive auth themselves; you don't.
  usage.mjs still runs on `[run timing]` without `user`, but say the figures are partial.
- "Cost-weighted" minutes are an `[estimate]`: GitHub's docs no longer state how macOS/Windows minutes count
  against included minutes. Say so when macOS or Windows dominates.

## References (read only when needed)

- `references/quotas.md`: every quota, rate and rule with source URL and read date, plus open conflicts. Read it
  when the user questions a number or asks whether a limit has changed.
- `references/risk-rules.md`: how each finding and saving is computed, and the bellboard worked example. Read it
  when explaining a finding.
- `references/endpoints.md`: the APIs used and the scopes they need. Read it when a call fails.

Thresholds (fine / watch / at risk) and risk tunables: `scripts/lib/config.json`. Tests:
`node --test 'scripts/test/*.test.mjs'`.

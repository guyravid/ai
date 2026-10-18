# Risk rules

Implemented in `scripts/lib/rules.mjs`, run by `scripts/risk.mjs`. Rates, defaults and limits come from
`quotas.md`; tunables (window, slack factors, docs patterns) live in `scripts/lib/config.json`.

## Method

- **History**: runs created in the last 30 days (`config.risk.windowDays`) and every job attempt of those runs
  (`jobs?filter=all`, so re-runs count). Each job is billed as its duration rounded up to the whole minute.
- **Cost-weighted minutes**: raw minutes x (runner rate / `actions_linux` rate), e.g. macOS 10.33x. GitHub no
  longer documents a multiplier, so the weight is derived from the rates and tagged `[estimate]`.
- **Monthly scaling**: observed total x 30 / observed days. Observed days = 30, or fewer if the repo is younger
  (from creation to now), with a minimum of 1 day. A young repo with a burst of activity therefore scales high.
  The report prints the observed days so you can judge this.
- **Per-run figures** are `[run timing]` averages. Monthly figures and savings are `[estimate]`.
- **Savings** are recorded per job attempt. "Combined" takes the largest saving any finding claims for each job
  attempt, capped at what that job billed, so overlapping changes are not double counted.
- **Workflow source**: every `.github/workflows/*.yml` on every branch. For a file with several versions, the
  version reviewed is the one on the branch of its most recent run, falling back to the default branch.
  `--local <dir>` compares a checkout with what's on GitHub and notes unpushed changes. History always reflects
  what ran on GitHub.

Output tiers: **findings** (cost observed in the window), **latent risks** (structure that can cost minutes,
with no cost observed yet), **checked** (nothing to do).

## Rules

| Rule | Detects | Minutes per run / month | Change proposed | Saving |
|---|---|---|---|---|
| Double trigger | `push` (not limited to the default branch) plus `pull_request` | Avg cost-weighted min per run; monthly cost of push runs whose `head_sha` also has a PR run | `push: branches: [<default>]` | Those duplicate push runs |
| Expensive runner | Job or matrix leg on a runner with weight > 1 (macOS, Windows, larger runners) | Avg weighted min per job run; monthly total | Matrix: build the leg list from `github.event_name` so the expensive leg runs only on `pull_request`. Plain job: `if: github.event_name == 'pull_request'` | Weighted minutes of non-PR job runs. Alternative printed: moving to `ubuntu-latest` saves weighted minus raw minutes, if the job isn't OS-specific |
| Matrix fan-out | `strategy.matrix` size; `fail-fast: false` | Weighted min per run across legs | `fail-fast: true` | Per failing run: sibling legs' minutes after the first failure (each re-rounded per job) |
| Paths filter | `push` / `pull_request` with no `paths` / `paths-ignore` | Avg per run | `paths-ignore:` with `config.risk.docsOnlyPatterns` | Runs whose changed files all match the docs patterns. Push: files since the previous run on that branch (first push: vs the default branch). PR: vs the default branch |
| Concurrency | No `concurrency` with `cancel-in-progress: true` | Avg per run | `concurrency: {group: ${{ github.workflow }}-${{ github.ref }}, cancel-in-progress: true}` | For each run superseded by a newer run (same branch and event, created before the older one finished): minutes after the newer one started |
| Timeouts | `timeout-minutes` missing (default 360) or more than 3x the longest observed run and above the suggested value | Exposure per hung run = timeout x weight | `timeout-minutes: max(2 x longest run, 10)` | Only if timed-out jobs were observed: (timeout - suggested) x weight per occurrence. Otherwise reported as latent exposure |
| Retries | Job attempts with `run_attempt > 1`; steps using a retry action | Weighted min of the extra attempts | Re-run failed jobs only; fix the flaky step | All re-run minutes (upper bound) |
| Schedules | `on.schedule` crons | Runs per 30 days (counted from the cron) x min per scheduled run | More than daily -> `cron: '0 6 * * *'` | (runs - 30) x min per run |
| Artifact retention | `actions/upload-artifact` without `retention-days` (default 90) or above 7 | Avg artifact GB; GB held at steady state = avg size x uploads per day x retention days | `retention-days: 7` | GB held at 90 vs 7 days (storage, not minutes) |
| Cache growth | Repo cache vs the 10 GB per-repo limit; keys duplicated across refs; bytes on `refs/pull/*` | n/a | Latent if at or above 80% of the limit (eviction can thrash) | n/a |

The rules don't parse anchors, aliases or reusable-workflow inputs. A called workflow's jobs are matched to the
caller job by the `"<caller> / <called>"` job name.

## Worked example: guyravid/bellboard (2026-10-17)

### Claim check

The repository is private. The default branch is `main`, which has **no** `.github/workflows` directory (the
contents API returns 404). Both workflows exist on `build/0001`. A local, unpushed commit `19292e2` (2026-10-17
13:31 -0400, "CI: run on pull requests, pushes to main and manually, not twice per branch push") changes the
triggers in ci.yml.

| Claim | Result | Evidence |
|---|---|---|
| `ci.yml` runs on both `push` and `pull_request` | partly | Pushed `origin/build/0001`: `ci.yml:10` `push:`, `ci.yml:11` `pull_request:` (all branches). Local HEAD `19292e2`: `ci.yml:12-13` `push: branches: [main]`, `ci.yml:14` `pull_request:`, `ci.yml:15` `workflow_dispatch:`. Not on `main` at all. |
| Test job matrix `[ubuntu-latest, macos-latest]` | confirmed | `ci.yml:31` (pushed) / `ci.yml:35` (local) `os: [ubuntu-latest, macos-latest]`, with `fail-fast: false` |
| `install` job on `macos-latest` | confirmed | `ci.yml:135-137` (pushed) / `ci.yml:139-141` (local) `install:` ... `runs-on: macos-latest` |
| `release.yml` runs on tags, on `macos-latest` | confirmed | `release.yml:7-9` `push: tags: ["v*.*.*"]`, `release.yml:20` `runs-on: macos-latest`. Never run: `gh workflow list --all` shows only `ci` |
| Recent run 39001297865 | confirmed | `gh run view 39001297865`: ci, push, `build/0001`, 2026-10-17T04:06:12Z, failure. Jobs: test (macos-latest) 3 m 57 s, install (macos-latest) 38 s, test (ubuntu-latest) 8 m 58 s. Two later runs exist: 39036201282 (push) and 39036203875 (pull_request), both on sha `684e261` |

### What the numbers show

- Billing API, October 2026: bellboard 25 Linux min + 14 macOS min. Adding up all 9 job attempts, each rounded up
  per job, gives exactly 25 + 14. This confirms the per-job rounding rule against real billing.
- Cost-weighted: 25 + 14 x 10.33 = 170 min. macOS is 36% of raw minutes but 85% of cost-weighted minutes.
- Runs 39036201282 (push) and 39036203875 (pull_request) are the same commit, which is the double trigger. Local
  commit `19292e2` already fixes it once pushed.
- In run 39036203875 the macOS leg failed at 13:24:23 and the Linux leg ran on to 13:30:15. With
  `fail-fast: false` that's 5 Linux min that `fail-fast: true` would have saved.
- `install` has `timeout-minutes: 20` against a longest run of 1 min, so a hang on macOS costs up to 207
  cost-weighted min.

# GitHub Actions quotas and billing rules

Every value the scripts use comes from this file (machine copy: `scripts/lib/quotas.json`).
All pages read on **2026-10-17**. Re-read the sources and diff against this file to spot changes on GitHub's side.

Sources (short names used below):

- [billing] https://docs.github.com/en/billing/concepts/product-billing/github-actions
- [pricing] https://docs.github.com/en/billing/reference/actions-runner-pricing
- [cycles] https://docs.github.com/en/billing/concepts/billing-cycles
- [concepts] https://docs.github.com/en/actions/concepts/billing-and-usage
- [jobtime] https://docs.github.com/en/actions/how-tos/monitor-workflows/view-job-execution-time
- [limits] https://docs.github.com/en/actions/reference/limits
- [runners] https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/choose-the-runner-for-a-job
- [cache] https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching
- [repo-settings] https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository

## Flags (need a human decision)

1. **Minute multipliers: the docs contradict each other.**
   - [billing] and [pricing] no longer list any multiplier. They give a per-minute USD rate per runner SKU and
     state included usage in minutes. Neither says how a macOS or Windows minute draws from the included minutes.
   - [concepts] says "GitHub Actions usage metrics do not apply minute multipliers to the metrics displayed", and
     [jobtime] says "The billable time shown does not include any minute multipliers". Both imply multipliers
     still exist, and both point back to [billing], which no longer documents any.
   - Not resolved here. See "Included-minute consumption" below for how the scripts handle it.
2. **Reset wording.** [billing] says both "free minutes reset to the full amount at the start of each billing
   cycle" and "At the start of each month, the minutes used by the account are reset to zero". [cycles] defines
   the metered billing period as the calendar month in UTC, so the two agree in practice. The personal account's
   *billing date* (the day the bill is charged) is a separate thing and doesn't affect the cycle.
3. **Retention policy change.** [repo-settings] says: "Starting October 1, 2026, these policies will apply to
   checks, workflow runs, and commit statuses." That date has passed. Artifact retention is unaffected.

## Included per plan (per month)

Source: [billing] table, matching [limits] storage table.

| Plan | Minutes | Artifact storage (shared with Packages) | Cache storage |
|---|---|---|---|
| GitHub Free | 2,000 min | 500 MB | 10 GB per repo |
| GitHub Pro | 3,000 min | 1 GB | 10 GB per repo |
| GitHub Free for organizations | 2,000 min | 500 MB | 10 GB per repo |
| GitHub Team | 3,000 min | 2 GB | 10 GB per repo |
| GitHub Enterprise Cloud | 50,000 min | 50 GB | 10 GB per repo |

## What is free

- Standard GitHub-hosted runners in **public** repositories, and self-hosted runners. [billing], [concepts]
- "There are no billable minutes when using GitHub Actions in public repositories or for jobs run on
  self-hosted runners." [jobtime]
- Standard runners for GitHub Pages and Dependabot. [billing]
- **Larger runners are never free**: "Larger runners are always charged for, even when used by public
  repositories or when you have quota available from your plan." [billing]. "Included minutes cannot be used
  for larger runners." [pricing]. Larger runners are only available on Team and Enterprise Cloud plans. [pricing]

## Rounding

- "GitHub rounds the minutes and partial minutes each job uses up to the nearest whole minute." [pricing]
  Rounding is **per job**, up. [jobtime] says the same: "rounded up to the next minute".
- Artifact storage is rounded to the nearest MB at month end. [billing]

## Billing cycle

- Metered products: "starts at 00:00:00 UTC on the first day of each month" and "ends at 23:59:59 UTC on the
  last day of the month". [cycles]
- Included minutes reset at the start of each cycle. Accrued storage charges also reset. [billing]

## Per-minute rates (standard runners, USD) [pricing], [billing]

| Runner | SKU | USD/min |
|---|---|---|
| Linux 1-core x64 (`ubuntu-slim`) | actions_linux_slim | 0.002 |
| Linux 2-core x64 | actions_linux | 0.006 |
| Linux 2-core arm64 | actions_linux_arm | 0.005 |
| Windows 2-core x64 | actions_windows | 0.010 |
| Windows 2-core arm64 | actions_windows_arm | 0.010 |
| macOS 3/4-core (M1 or Intel) | actions_macos | 0.062 |

Larger runner SKUs (Team/Enterprise only) are listed in [pricing]. They're kept in `quotas.json` under
`largerRunnerRates` so the risk review can price `runs-on` labels that ask for one.

## Runner labels -> SKU (private repositories) [runners]

| Labels | SKU |
|---|---|
| `ubuntu-slim` | actions_linux_slim |
| `ubuntu-latest`, `ubuntu-24.04`, `ubuntu-22.04`, `ubuntu-26.04` | actions_linux |
| `ubuntu-24.04-arm`, `ubuntu-22.04-arm`, `ubuntu-26.04-arm` | actions_linux_arm |
| `windows-latest`, `windows-2025`, `windows-2022` | actions_windows |
| `windows-11-arm`, `windows-11-vs2026-arm` | actions_windows_arm |
| `macos-latest`, `macos-14`, `macos-15`, `macos-26`, `xcode-27`, `macos-15-intel`, `macos-26-intel` | actions_macos |

## Included-minute consumption (see flag 1)

The docs no longer document a per-OS multiplier. The scripts therefore:

- show **raw minutes** per SKU (rounded per job) as documented;
- show a **cost-weighted multiplier** for comparison: SKU rate / `actions_linux` rate (for example
  macOS 0.062 / 0.006 = 10.33). It's tagged `[estimate]` because it's derived, not documented;
- prefer the billing API's own `quantity` / `discountAmount` / `netAmount` whenever it's available, because
  that is what GitHub actually charged against the quota.

**Decision (2026-10-17): `multiplierMode = "billing-api-then-cost-weighted"`.**
What the API shows: on 2026-10-17 the user billing usage API returned `quantity` as raw minutes per SKU
("Actions Linux" 25 min at $0.006, "Actions macOS 3-core" 14 min at $0.062), with `discountAmount` equal to
`grossAmount`, meaning all of it was covered by included usage. The API doesn't return the remaining included
quota, so it can't settle how a macOS minute counts against the 2,000. The "Billed min" column therefore uses the
cost-weighted fallback (rate / `actions_linux` rate, tagged `[estimate]`). Raw minutes and dollar amounts are
shown as returned (`[billing API]`). The real API SKU names are display names ("Actions Linux", "Actions macOS 3-core");
the script maps them by keyword (linux / windows / macos, plus arm / slim).

## Storage [billing], [cache], [repo-settings]

- Measured in binary GB (1 GB = 2^30 bytes), billed in GB-hours. The monthly bill converts to GB-months
  using hours in the month (usually 720 for a 30-day month). Charges accrue hourly; deleting artifacts stops
  future accrual but doesn't refund what has already accrued.
- Artifact storage is shared with GitHub Packages. Logs and job summaries don't count.
- Cache: 10 GB per repo by default, measured as peak usage per hour. It's billed only above 10 GB, and only if
  the repo's cache limit is raised above the included amount.
- Cache eviction: entries not accessed in over 7 days are removed. Over the size limit, the oldest-accessed
  entries are evicted first, which can cause "cache thrashing". [cache]
- Artifact and log retention: default 90 days. Configurable 1-90 days for public repos and 1-400 days for
  private repos. [repo-settings]
- Cache retention: default 7 days. Configurable up to 90 days (public) or 365 days (private). [repo-settings]

## Overage rates (reference only; the skill reports against included amounts)

- Artifact/Packages storage: $0.25 per GB-month. Actions cache: $0.07 per GB-month. [billing]

## Limits and workflow defaults relevant to risk review

[syntax] https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax

- Job execution limit on GitHub-hosted runners: 6 hours per job. [limits]
- `jobs.<job_id>.timeout-minutes`: "Default: 360". [syntax]
- `strategy.fail-fast` "defaults to `true`". With no `max-parallel`, GitHub runs as many matrix jobs in
  parallel as runners allow. [syntax]
- `paths` filters: the workflow runs only if at least one changed file matches. `concurrency` with
  `cancel-in-progress: true` cancels the running job or workflow in the same group. [syntax]
- The shortest `schedule` interval is once every 5 minutes. [syntax]
- Usage notifications are sent at 90% and 100% of included usage. [billing]

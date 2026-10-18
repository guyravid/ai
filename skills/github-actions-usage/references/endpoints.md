# Endpoints

All calls go through `gh api` (GET only), `gh run`, `gh workflow`, `gh repo`. No tokens are read or stored.
Docs read on 2026-10-17. Scope check: `gh auth status`. A refused call prints the scope it needs, e.g.
`gh: This API operation needs the "user" scope`.

## Account usage (preferred source, tag [billing API])

| Endpoint | Returns | Scope | Notes |
|---|---|---|---|
| `GET /users/{username}/settings/billing/usage?year=&month=&day=` | `usageItems[]`: `date, product, sku, quantity, unitType, pricePerUnit, grossAmount, discountAmount, netAmount, repositoryName` | classic `user` (confirmed by gh's 404 message) | "only available to users with access to the enhanced billing platform". Fine-grained PATs are not supported. Per-day, per-repo, per-SKU rows; `discountAmount` is the part covered by included usage. Source: https://docs.github.com/en/rest/billing/usage |
| `GET /users/{username}/settings/billing/usage/summary?year=&month=&repository=&product=&sku=` | `timePeriod, user, repository, product, sku, usageItems[]: product, sku, unitType, pricePerUnit, grossQuantity, grossAmount, discountQuantity, discountAmount, netQuantity, netAmount` | classic `user` | **Public preview, "subject to change"**. Used only as a cross-check. |
| `GET /organizations/{org}/settings/billing/usage` (and `/summary`) | Same as the user endpoints, plus `organizationName` | org owner or billing manager | For org accounts. Not used for `guyravid`. |

Observed 2026-10-17 (personal account `guyravid`, plan free): rows are one per day per repo per SKU; `sku` is a
display name ("Actions Linux", "Actions macOS 3-core"), `quantity` is raw minutes rounded per job (it matched the
per-job sum from the jobs API exactly), and `discountAmount` = `grossAmount` while inside the included usage.
No storage rows appeared this cycle (no artifacts), so the storage row shape is unverified: the script treats
any non-`Minutes` row with an hour-based `unitType` as GB-hours.

Summary endpoints return 24 months of history at most. The enhanced-billing tutorial
(https://docs.github.com/en/billing/tutorials/automate-usage-reporting) confirms that classic PATs are required
and gives no scope name; the `user` scope requirement comes from gh's error message.

## Plan (to choose included amounts)

| Endpoint | Returns | Scope | Notes |
|---|---|---|---|
| `GET /user` | `plan: {name, space, private_repos, collaborators}` | `read:user` or `user` for the private response | Without the scope, `plan` comes back `null` (observed). Fallback: `--plan` flag. Source: https://docs.github.com/en/rest/users/users |

## Per-run timing (fallback and breakdown, tag [run timing])

| Endpoint | Returns | Scope | Notes |
|---|---|---|---|
| `GET /repos/{o}/{r}/actions/runs?created=>=YYYY-MM-DD&per_page=100` | runs: `id, event, head_branch, workflow_id, name, path, run_attempt, created_at, conclusion` | `repo` (private) | Filters: `created`, `event`. https://docs.github.com/en/rest/actions/workflow-runs |
| `GET /repos/{o}/{r}/actions/runs/{id}/jobs?filter=all` | jobs: `name, workflow_name, labels, runner_name, run_attempt, started_at, completed_at, conclusion` | `repo` (private) | Used to compute per-job duration, rounded up per job. `filter=all` includes re-run attempts, which are billed too. https://docs.github.com/en/rest/actions/workflow-jobs |
| `GET /repos/{o}/{r}/actions/runs/{id}/timing` | `billable.{UBUNTU,MACOS,WINDOWS}.total_ms/jobs/job_runs`, `run_duration_ms` | `repo` (private) | **"This endpoint is in the process of closing down."** Not used. |
| `gh run list -R o/r --json ...` / `gh run view <id> -R o/r --json jobs` | Wrappers over the above | `repo` | Convenience. |

## Storage

| Endpoint | Returns | Scope | Notes |
|---|---|---|---|
| `GET /repos/{o}/{r}/actions/artifacts?per_page=100` | `artifacts[]: name, size_in_bytes, expired, created_at, expires_at, workflow_run` | `repo` (private) | Current (non-expired) artifact bytes. A point-in-time figure, not GB-hours. https://docs.github.com/en/rest/actions/artifacts |
| `GET /repos/{o}/{r}/actions/cache/usage` | `active_caches_size_in_bytes, active_caches_count` | `repo` (private) | https://docs.github.com/en/rest/actions/cache |
| `GET /repos/{o}/{r}/actions/caches` | `actions_caches[]: key, ref, size_in_bytes, last_accessed_at, created_at` | `repo` | For cache growth and per-ref keys. |

## Repos and workflows

| Command | Use |
|---|---|
| `gh repo list <owner> --limit 1000 --json nameWithOwner,visibility,isArchived,pushedAt` | Repo list, public vs private, repo picker |
| `gh workflow list -R o/r --all --json name,path,state,id` | Workflows (only those that have run, or are on the default branch) |
| `gh api repos/{o}/{r}/branches` | Branches to scan for workflow files |
| `gh api repos/{o}/{r}/compare/{base}...{head}` | Files changed by a run's push or PR (paths-filter rule) |
| `gh api /repos/{o}/{r}/contents/.github/workflows?ref=<branch>` and `.../contents/<path>?ref=<branch>` | Workflow YAML per branch (base64 `content`) |

## Scope validation

Both scripts read the token's scopes from the `X-OAuth-Scopes` header of `gh api -i user` and print a NOTICE
with the exact `gh auth refresh -h github.com -s <scope>` command when one is missing: `user` (usage.mjs:
billing API and plan) and `repo` (both: private repos). Without `user`, usage.mjs falls back to `[run timing]`
minutes and `--plan` (default `free`). A refused call that names a scope also produces the notice.

# Phase 2: Cloud Account Sync

Read this file when the user opts into cloud account sync after Phase 1 completes.

## Authentication

Ask for their Anthropic API key if not already set in the environment (`$ANTHROPIC_API_KEY`). The key is only used for this session and never stored by the skill.

If the user doesn't know where to find their API key:

1. Go to [console.anthropic.com](https://console.anthropic.com)
2. Sign in → **Settings → API Keys**
3. Click **"Create Key"** or copy an existing key
4. Paste it — it starts with `sk-ant-`

## Step A: Fetch cloud skills

Call the Skills API to list all custom skills. Paginate using `next_page` until `has_more` is false:

```bash
curl -s "https://api.anthropic.com/v1/skills?source=custom&limit=100" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: skills-2025-10-02"
```

For each skill, fetch version metadata to get `name` and `description`:

```bash
curl -s "https://api.anthropic.com/v1/skills/{skill_id}/versions/{latest_version}" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: skills-2025-10-02"
```

Build a map of cloud skills keyed by `name`.

## Step B: Compare with local

Compare cloud skills against the local target from Phase 1. Identify:

- **Cloud only** — in cloud, not installed locally
- **Local only** — installed locally, not in cloud
- **Both** — present in both (no action needed)

Present discrepancies clearly before doing anything.

## Step C: Cloud → Local (download)

For cloud-only skills, offer to create local stubs. The API returns metadata only — not the full skill body.

For each cloud-only skill the user accepts, create `<target>/<name>/SKILL.md`:

```markdown
---
name: <name>
description: <description>
---

# <name>

> **Note:** Downloaded from Anthropic cloud account.
> Full skill instructions were not available via the API.
> Fill in the body, or find the original source to restore the complete skill.
```

Tell the user clearly that stubs are placeholders requiring manual completion.

## Step D: Local → Cloud (package for upload)

The API has no automated upload path. For each local-only skill, package it:

```bash
bash scripts/package_skill.sh <skill-dir>
```

Then give the user these upload instructions:

1. Go to [claude.ai](https://claude.ai) → Settings → Skills
2. Click **"Add skill"** / **"Upload skill"**
3. Select the `.skill` file(s) from `/tmp/`
4. Give each skill a display title when prompted

## Phase 2 Summary

After handling discrepancies, present a combined summary:

| Skill | Phase | Action |
|-------|-------|--------|
| `data-analyst` | Cloud → Local | Stub created |
| `my-custom-skill` | Local → Cloud | Packaged as `.skill` |
| `code-reviewer` | Cloud → Local | Already up to date |

---
name: design-trail
description: Turn a design or brainstorming conversation into a docs/ trail an agent can re-read from the start to understand how a project's design came to be — origin materials kept verbatim, a chronological thought process, numbered decision records, research findings, open questions, and a docs/README.md index. Use whenever the user wants to "store the docs", "write up the thought process", "capture the brainstorm", "document how we got here", seed a new repo with the design history, or record decisions/ADRs from a session — including from a past session by its ID — even if they don't say "design trail". Also use to update an existing trail after a follow-up session.
---

# Design trail

Builds (or updates) a `docs/` folder whose purpose is **agentic re-reading**: a future agent opening the repo cold should be able to follow the reasoning in order, see what was decided and why, what was rejected, and what is still open — without the original conversation.

The trail never creates or edits `CLAUDE.md`. Repos keep `CLAUDE.md` slim and about the repo's role; the entry point is `docs/README.md`.

## Layout

```
docs/
├── README.md               # entry point: what/why in 3 lines, status, reading order, indexes
├── 00-origin/              # source material, verbatim: tickets, attached plans, briefs
│   └── session.md          # session id(s) + transcript path(s) for the raw discussion
├── 01-thought-process.md   # chronological stages: trigger → considered → changed → why
├── decisions/NNNN-<slug>.md
├── research/<topic>.md     # verified facts with source, version and date
└── open-questions.md       # decisions pending, facts to verify, experiments
```

## 1. Establish inputs

- **Target root**: the repo the user names. If it doesn't exist yet, ask; offer to draft into the scratchpad and copy later.
- **Conversation source**: this session by default. For another session, get its ID (or .jsonl path) and run:
  `python3 scripts/extract_transcript.py --session-id <id> --out <scratchpad>/transcript.md`
  With no arguments it reads `$CLAUDE_CODE_SESSION_ID`. It keeps human prompts (including messages sent mid-turn), assistant replies, answers to questions asked via AskUserQuestion, and one line of tool activity per turn; it drops thinking, tool output and system noise. Read the output instead of the raw .jsonl. For the current session your own context is usually enough; still run it to get the session metadata for `session.md`.
- **Origin materials**: tickets, attached files, briefs the discussion started from. Fetch them with the tools available (MCP first). If one can't be fetched, ask the user for a local copy rather than reconstructing it from memory.

## 2. Scaffold

`python3 scripts/scaffold.py --root <target> --title "<project or working title>"`

Creates missing folders and skeleton files from `assets/templates/`, never overwrites, and prints JSON of `created` / `existing`. Existing files mean **update mode** (section 4).

## 3. Write the content

Templates in `assets/templates/` show each file's shape; `decision.md` and `research.md` are copied per record by you, not by the scaffold.

- **00-origin/**: copy source material **verbatim**, each file headed with its source (URL/path) and retrieval date. Don't summarise here — the point is that later readers can check the starting assumptions themselves.
- **session.md**: session id, transcript path, date range, working directory, and the `claude --resume <id>` command.
- **01-thought-process.md**: one stage per real shift in the design, in conversation order. Each stage: what triggered it (quote the user's question briefly), what was considered, what changed, why, and links to the decision records it produced. Keep rejected and superseded ideas with the reason — a re-reader needs to know why the obvious alternative wasn't taken. Mark superseded content instead of deleting it.
- **decisions/**: one record per decision, numbered from `0001`. Status is `accepted` only when the user agreed; your recommendations that weren't confirmed are `proposed`; overturned ones become `superseded` (link the replacement) or `rejected`. Getting status right matters more than prose — a future agent will treat `accepted` as binding.
- **research/**: facts established during the session, grouped by topic, each with its source (doc, file path, command, API version) and date. Label anything inferred or unverified as such.
- **open-questions.md**: three sections — decisions for the user (with your recommendation), facts to verify, experiments/tests. Drop items the conversation settled; move them into decisions.
- **README.md**: fill the template — what and why in ≤3 lines, current status, the reading order, a decision table (number, title, status) and counts of open items. A placeholder project name is fine.

Attribute carefully: what the user said or decided vs. what you suggested. Don't invent decisions the conversation didn't reach.

## 4. Update mode

When `docs/` already has a trail: append new stages to `01-thought-process.md`, add new decision records (next number), set `superseded`/`rejected` on records the new session overturned, revise `open-questions.md` and `README.md`, add the new session to `session.md`. Don't rewrite earlier stages — history is the point.

## 5. Report

Tell the user what was created or updated (paths), anything you couldn't fetch, and the decisions you recorded as `proposed` so they can confirm or correct them. Mention that a one-line pointer to `docs/README.md` can be added to `CLAUDE.md` if they want it, but don't add it.

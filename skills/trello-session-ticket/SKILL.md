---
name: trello-session-ticket
description: Create a Trello card for the current Claude Code session with a summary of the work, the session id, the transcript path, and the exact command to resume it. Use when the user wants to save, park, bookmark, log, or ticket the current session, says "make a ticket for this session", "save this to Trello so I can come back to it", "park this", or anything about returning to this conversation later via Trello.
---

# Trello session ticket

Creates one card in Trello that lets the user find and resume this session later.

Target is always board **ToDo** (`6aa86ef80a64c31096a786e2`), list **AI Session** (`6ab8296c1da3780ff3387e9e`). Session tickets never go to another board or list, and the "To Do"-style lists on other boards are not substitutes.

## 1. Collect session facts

Run in one Bash call:

```bash
echo "SESSION_ID=$CLAUDE_CODE_SESSION_ID"
ls ~/.claude/projects/*/"$CLAUDE_CODE_SESSION_ID".jsonl
pwd
git rev-parse --abbrev-ref HEAD 2>/dev/null
date +%Y-%m-%d
```

- The `ls` result is the session path. If `CLAUDE_CODE_SESSION_ID` is empty or the file is missing, stop and tell the user rather than guessing.
- Branch is optional; omit it outside a git repo.

## 2. Write the card

**Title**: `<short task name> (<YYYY-MM-DD>)`, under ~70 characters, naming the work, not the tool.

**Description** (Markdown), from your own knowledge of the conversation so far:

```markdown
## Summary
<2-4 sentences: the goal and where things stand>

## Done
- <concrete outcomes, with file paths where relevant>

## Open / next steps
- <what remains, decisions pending, known issues; "None" if finished>
```

**Resume comment** (Markdown), posted as a comment on the card, not in the description:

```markdown
## Resume
- **Session ID:** `<id>`
- **Session path:** `<path to .jsonl>`
- **Working directory:** `<cwd>`
- **Branch:** `<branch>`

    cd <cwd> && claude --resume <id>
```

Be factual. Don't claim something was verified or finished unless it was.

## 3. Preview, then create

Write the description and the resume comment to files in the scratchpad directory (avoids shell-quoting problems), then show the user the title, description, and resume comment and ask for a go-ahead. Creating a card is an outward-facing write; never skip this unless the user already said to create it without review.

On approval:

```bash
trello cards create \
  --list-id <list id> \
  --name "<title>" \
  --desc "$(cat <scratchpad>/session-ticket.md)" \
  --pos top \
  --confirm

trello cards comment <card id from the create result> \
  --text "$(cat <scratchpad>/session-resume.md)" \
  --confirm
```

- Post the comment only after the create returns `ok:true`. If the comment fails, the card already exists: report it and retry the comment alone, never the create.
- Check `ok` in each JSON envelope. On `ok:false`, report `error.code` and `error.message`; for `auth`, `config`, or `network`, run `trello doctor` and report. On `timeout`, do not retry blindly: check with `trello cards search --query "<session id>"` first.
- On success, reply with the card's `shortUrl` and the resume command. Nothing else.

## Updating an existing ticket

If the user asks to update the ticket for this session rather than create a new one, find it with `trello cards search --query "<session id>" --fields id,name,shortUrl`. `cards update` replaces the whole description, so rebuild the full description (same template, without the resume section) and run `trello cards update <card> --desc "..." --confirm`. Use `trello cards comment <card> --text "..." --confirm` instead if they only want to log progress. Run `trello describe cards.update` / `cards.comment` if a flag is unclear.

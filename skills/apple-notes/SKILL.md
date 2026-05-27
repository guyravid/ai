---
name: apple-notes
description: Read, create, and edit Apple Notes on macOS using AppleScript. Use this skill whenever the user wants to interact with their notes — reading a note's content, creating new notes, updating or editing existing notes, adding sections, listing notes in a folder, or generating notes from a template. Trigger for any request involving "my notes", "the note", "Apple Notes", a named note, or a folder of notes — even if the user doesn't say "Apple Notes" explicitly.
---

Apple Notes is accessed via AppleScript through `osascript`. All note content is HTML.
Use `scripts/note_ops.py` for all operations — it handles script generation, temp files,
and AppleScript escaping, and gives clear errors if Notes is unavailable.

## HTML Structure

Before generating any note body HTML, read `references/html-structure.md`. It defines the
exact mapping between Notes UI elements (Title, Heading, Subheading, Body) and their HTML
forms. Getting this right matters — wrong tag nesting renders incorrectly in Notes.

## Script Path

```
SKILL=~/.claude/skills/apple-notes
python $SKILL/scripts/note_ops.py <command> [options]
```

## Operations

### Read a note
```bash
python $SKILL/scripts/note_ops.py read --folder "Hiring" --note "Senior SRE (Template)"
```
Output: raw HTML. Strip `<img src="data:image/..."/>` blocks before showing to the user.

### List notes in a folder
```bash
python $SKILL/scripts/note_ops.py list --folder "Hiring"
```

### Create a note
Write the HTML body to a temp file first, then:
```bash
cat > /tmp/note_body.html << 'EOF'
<html body here>
EOF
python $SKILL/scripts/note_ops.py create \
  --folder "Hiring" --title "Junior SRE" --body-file /tmp/note_body.html
```

### Update a note (replaces full body)
Read the current body first if you need to preserve content, make targeted edits, then:
```bash
python $SKILL/scripts/note_ops.py update \
  --folder "Hiring" --note "Senior SRE (Template)" --body-file /tmp/note_body.html
```

## Workflow for partial edits

When asked to add or change content within a section (e.g., "add X to General Notes"):
1. Read the note body with `read`
2. Locate the target heading `<div>` in the HTML
3. Insert/replace the relevant `<div>` elements after it
4. Write the full updated HTML to `/tmp/note_update.html`
5. Run `update` with `--body-file /tmp/note_update.html`

Sections are flat — delimited by the next `<h2>` heading div or end of string. There is
no nesting.

## Error handling

The script exits non-zero and prints a clear message if:
- Notes.app is not accessible (permission or platform issue)
- The specified folder doesn't exist
- The specified note doesn't exist

If you hit an access error, tell the user to check:
System Settings → Privacy & Security → Automation → allow Terminal/Claude to control Notes.

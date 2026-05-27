#!/usr/bin/env python3
"""Apple Notes operations via AppleScript/osascript."""

import argparse
import os
import subprocess
import sys
import tempfile


def _check_notes_available() -> None:
    """Verify Notes app is accessible via AppleScript."""
    result = subprocess.run(
        ["osascript", "-e", 'tell application "Notes" to return name of first account'],
        capture_output=True, text=True, timeout=10
    )
    if result.returncode != 0:
        print(
            "Error: Apple Notes is not accessible. Make sure:\n"
            "  - You are on macOS\n"
            "  - Notes.app is installed\n"
            "  - Automation permissions are granted (System Settings > Privacy & Security > Automation)\n"
            f"  Details: {result.stderr.strip()}",
            file=sys.stderr
        )
        sys.exit(1)


def _run_script(script: str) -> str:
    """Write script to a temp file and run via osascript."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".applescript", delete=False) as f:
        f.write(script)
        script_path = f.name
    try:
        result = subprocess.run(
            ["osascript", script_path],
            capture_output=True, text=True, timeout=30
        )
        if result.returncode != 0:
            print(f"AppleScript error: {result.stderr.strip()}", file=sys.stderr)
            sys.exit(1)
        output = result.stdout.strip()
        if output.startswith("ERROR:"):
            print(output, file=sys.stderr)
            sys.exit(1)
        return output
    except subprocess.TimeoutExpired:
        print("Error: Notes operation timed out after 30s.", file=sys.stderr)
        sys.exit(1)
    finally:
        os.unlink(script_path)


def _escape_as_string(value: str) -> str:
    """Escape a string for safe embedding in an AppleScript string literal."""
    return value.replace("\\", "\\\\").replace('"', '\\"')


def _strip_title_div(html: str) -> str:
    """Strip any leading <h1> div and the auto-generated Body-style name div from HTML.

    On write, Notes converts our <h1> to a 24px bold span (Title visual) AND
    auto-appends a plain Body-style <div>name</div> after it. Stripping both on
    the second-pass write leaves exactly one prominent title with no duplicate.
    """
    import re
    # Strip <h1> title div
    html = re.sub(r"^\s*<div><h1>.*?</h1></div>\s*", "", html, count=1, flags=re.DOTALL)
    # Strip the 24px bold span that Notes converts h1 into (present on second-pass reads)
    html = re.sub(r"^\s*<div><b><span style=\"font-size: 24px\">.*?</span></b></div>\s*", "", html, count=1, flags=re.DOTALL)
    return html


def _read_body(folder: str, note: str) -> str:
    note_safe = _escape_as_string(note)
    script = f"""tell application "Notes"
{_folder_lookup_script(folder)}
  repeat with n in notes of targetFolder
    if name of n is "{note_safe}" then
      return body of n
    end if
  end repeat
  return "ERROR: Note \\"{note_safe}\\" not found"
end tell"""
    return _run_script(script)


def _write_body(folder: str, note: str, body_html: str) -> str:
    note_safe = _escape_as_string(note)
    with tempfile.NamedTemporaryFile(mode="w", suffix=".html", delete=False, encoding="utf-8") as f:
        f.write(body_html)
        tmp_body = f.name
    script = f"""tell application "Notes"
{_folder_lookup_script(folder)}
  set newBody to do shell script "cat {tmp_body}"
  repeat with n in notes of targetFolder
    if name of n is "{note_safe}" then
      set body of n to newBody
      return "OK"
    end if
  end repeat
  return "ERROR: Note \\"{note_safe}\\" not found"
end tell"""
    try:
        return _run_script(script)
    finally:
        os.unlink(tmp_body)


def _fix_title(folder: str, note: str) -> None:
    """Two-pass title fix: strip the auto-generated Body-style name div that
    Notes adds alongside our h1-converted 24px Title span.

    Notes produces two different orderings depending on the operation:
    - update: [24px span] then [auto Body div]  → strip the trailing Body div
    - create: [auto Body div] then [24px span]  → strip the leading Body div
    """
    import re
    body = _read_body(folder, note)

    title_span = r'<div><b><span style="font-size: 24px">.*?</span></b>(?:<br>)?</div>'
    plain_div  = r'<div>[^<\n]+</div>'

    # update order: 24px span first, plain div second
    cleaned = re.sub(
        rf'({title_span})\s*{plain_div}\s*',
        r'\1\n',
        body, count=1, flags=re.DOTALL
    )
    # create order: plain div first, 24px span second
    if cleaned == body:
        cleaned = re.sub(
            rf'{plain_div}\s*({title_span})\s*',
            r'\1\n',
            body, count=1, flags=re.DOTALL
        )

    if cleaned != body:
        _write_body(folder, note, cleaned)


def _folder_lookup_script(folder: str) -> str:
    """AppleScript fragment to find a folder by name across all accounts."""
    folder_safe = _escape_as_string(folder)
    return f"""
  set targetFolder to missing value
  repeat with acct in accounts
    repeat with f in folders of acct
      if name of f is "{folder_safe}" then
        set targetFolder to f
        exit repeat
      end if
    end repeat
    if targetFolder is not missing value then exit repeat
  end repeat
  if targetFolder is missing value then
    return "ERROR: Folder \\"{folder_safe}\\" not found"
  end if"""


def cmd_read(folder: str, note: str) -> None:
    note_safe = _escape_as_string(note)
    script = f"""tell application "Notes"
{_folder_lookup_script(folder)}
  repeat with n in notes of targetFolder
    if name of n is "{note_safe}" then
      return body of n
    end if
  end repeat
  return "ERROR: Note \\"{note_safe}\\" not found in folder \\"{_escape_as_string(folder)}\\""
end tell"""
    print(_run_script(script))


def cmd_list(folder: str) -> None:
    script = f"""tell application "Notes"
{_folder_lookup_script(folder)}
  set noteNames to {{}}
  repeat with n in notes of targetFolder
    set end of noteNames to name of n
  end repeat
  set AppleScript's text item delimiters to linefeed
  return noteNames as text
end tell"""
    result = _run_script(script)
    print(result if result else "(no notes in folder)")


def cmd_create(folder: str, title: str, body_file: str) -> None:
    with open(body_file, encoding="utf-8") as f:
        body_html = f"<div><h1>{title}</h1></div>\n" + _strip_title_div(f.read())

    with tempfile.NamedTemporaryFile(mode="w", suffix=".html", delete=False, encoding="utf-8") as f:
        f.write(body_html)
        tmp_body = f.name

    title_safe = _escape_as_string(title)
    script = f"""tell application "Notes"
{_folder_lookup_script(folder)}
  set noteBody to do shell script "cat {tmp_body}"
  make new note at targetFolder with properties {{name:"{title_safe}", body:noteBody}}
  return "Created: {title_safe}"
end tell"""
    try:
        _run_script(script)
    finally:
        os.unlink(tmp_body)
    _fix_title(folder, title)
    print(f"Created: {title}")


def cmd_update(folder: str, note: str, body_file: str) -> None:
    with open(body_file, encoding="utf-8") as f:
        body_html = f"<div><h1>{note}</h1></div>\n" + _strip_title_div(f.read())

    _write_body(folder, note, body_html)
    _fix_title(folder, note)
    print(f"Updated: {note}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Apple Notes operations via AppleScript")
    sub = parser.add_subparsers(dest="command", required=True)

    r = sub.add_parser("read", help="Print raw HTML body of a note")
    r.add_argument("--folder", required=True, help="Folder name (case-sensitive)")
    r.add_argument("--note", required=True, help="Note name (case-sensitive)")

    ls = sub.add_parser("list", help="List note names in a folder")
    ls.add_argument("--folder", required=True)

    c = sub.add_parser("create", help="Create a new note from an HTML file")
    c.add_argument("--folder", required=True)
    c.add_argument("--title", required=True)
    c.add_argument("--body-file", required=True, help="Path to HTML file with note body")

    u = sub.add_parser("update", help="Replace a note's body from an HTML file")
    u.add_argument("--folder", required=True)
    u.add_argument("--note", required=True)
    u.add_argument("--body-file", required=True, help="Path to HTML file with new body")

    args = parser.parse_args()
    _check_notes_available()

    if args.command == "read":
        cmd_read(args.folder, args.note)
    elif args.command == "list":
        cmd_list(args.folder)
    elif args.command == "create":
        cmd_create(args.folder, args.title, args.body_file)
    elif args.command == "update":
        cmd_update(args.folder, args.note, args.body_file)


if __name__ == "__main__":
    main()

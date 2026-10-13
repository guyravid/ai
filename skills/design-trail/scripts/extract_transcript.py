#!/usr/bin/env python3
"""Extract the conversation from a Claude Code session .jsonl into readable Markdown."""

import argparse
import json
import os
import re
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Optional

Record = dict[str, Any]

SYSTEM_REMINDER = re.compile(r"<system-reminder>.*?</system-reminder>", re.DOTALL)
MARKDOWN_HEADING = re.compile(r"^(#{1,6})\s+(.*)$")
HEADING_DEMOTION = 2
PROJECTS_DIR = Path.home() / ".claude" / "projects"


class TranscriptError(Exception):
    pass


@dataclass
class Turn:
    prompt: Optional[str] = None
    events: list[str] = field(default_factory=list)  # rendered Markdown blocks, in order
    tool_counts: Counter = field(default_factory=Counter)
    bash_descriptions: list[str] = field(default_factory=list)


@dataclass
class Metadata:
    session_id: str = ""
    path: str = ""
    cwd: str = ""
    first_timestamp: str = ""
    last_timestamp: str = ""


def find_session_file(session_id: str, projects_dir: Path = PROJECTS_DIR) -> Path:
    matches = sorted(projects_dir.glob(f"*/{session_id}.jsonl"))
    if not matches:
        raise TranscriptError(f"no transcript found for session id {session_id} under {projects_dir}")
    if len(matches) > 1:
        listing = ", ".join(str(match) for match in matches)
        raise TranscriptError(f"session id {session_id} is ambiguous, use --path: {listing}")
    return matches[0]


def resolve_transcript_path(session_id: Optional[str], path: Optional[str], projects_dir: Path = PROJECTS_DIR) -> Path:
    if path:
        file = Path(path).expanduser()
        if not file.is_file():
            raise TranscriptError(f"transcript file does not exist: {file}")
        return file
    session_id = session_id or os.environ.get("CLAUDE_CODE_SESSION_ID")
    if not session_id:
        raise TranscriptError("provide --session-id or --path, or set CLAUDE_CODE_SESSION_ID")
    return find_session_file(session_id, projects_dir)


def read_records(file: Path) -> list[Record]:
    records: list[Record] = []
    with file.open(encoding="utf-8", errors="replace") as handle:
        for line in handle:
            try:
                record = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(record, dict):
                records.append(record)
    return records


def strip_system_reminders(text: str) -> str:
    return SYSTEM_REMINDER.sub("", text).strip()


def is_countable(record: Record) -> bool:
    return not record.get("isSidechain") and not record.get("isMeta")


def content_blocks(record: Record) -> list[Record]:
    content = record.get("message", {}).get("content")
    if not isinstance(content, list):
        return []
    return [block for block in content if isinstance(block, dict)]


def tool_result_text(block: Record) -> str:
    content = block.get("content")
    if isinstance(content, str):
        return content.strip()
    if isinstance(content, list):
        parts = [item.get("text", "") for item in content if isinstance(item, dict) and item.get("type") == "text"]
        return "\n".join(parts).strip()
    return ""


def demote_headings(text: str) -> str:
    """Push Markdown headings below the transcript's own `## Turn N` level so turns stay delimited.

    Lines inside fenced code blocks are left alone, since `#` there is usually a comment.
    """
    lines: list[str] = []
    in_fence = False
    for line in text.splitlines():
        if line.lstrip().startswith("```"):
            in_fence = not in_fence
        match = None if in_fence else MARKDOWN_HEADING.match(line)
        if match:
            level = min(len(match.group(1)) + HEADING_DEMOTION, 6)
            line = "#" * level + " " + match.group(2)
        lines.append(line)
    return "\n".join(lines)


def render_quote(text: str) -> str:
    return "\n".join(f"> {line}" if line else ">" for line in text.splitlines())


def summarize_tools(turn: Turn) -> str:
    if not turn.tool_counts:
        return ""
    parts: list[str] = []
    for name, count in turn.tool_counts.items():
        part = f"{name}×{count}"
        if name == "Bash" and turn.bash_descriptions:
            part += f" ({'; '.join(turn.bash_descriptions)})"
        parts.append(part)
    return "Tools: " + ", ".join(parts)


def collect_metadata(records: list[Record], path: Path) -> Metadata:
    metadata = Metadata(session_id=path.stem, path=str(path))
    timestamps = [record["timestamp"] for record in records if isinstance(record.get("timestamp"), str)]
    if timestamps:
        metadata.first_timestamp, metadata.last_timestamp = min(timestamps), max(timestamps)
    metadata.cwd = next((record["cwd"] for record in records if record.get("cwd")), "")
    return metadata


def build_turns(records: list[Record]) -> list[Turn]:
    turns: list[Turn] = []
    ask_tool_ids: set[str] = set()

    def current_turn() -> Turn:
        if not turns:
            turns.append(Turn())
        return turns[-1]

    for record in records:
        record_type = record.get("type")
        if record_type == "attachment":
            attachment = record.get("attachment", {})
            if attachment.get("type") == "queued_command" and not record.get("isSidechain"):
                text = strip_system_reminders(str(attachment.get("prompt", "")))
                if text:
                    current_turn().events.append("**User (sent mid-turn):**\n\n" + render_quote(text))
            continue
        if not is_countable(record):
            continue
        if record_type == "assistant":
            handle_assistant(record, current_turn(), ask_tool_ids)
        elif record_type == "user":
            handle_user(record, turns, current_turn, ask_tool_ids)
    return turns


def handle_assistant(record: Record, turn: Turn, ask_tool_ids: set[str]) -> None:
    for block in content_blocks(record):
        block_type = block.get("type")
        if block_type == "text":
            text = strip_system_reminders(str(block.get("text", "")))
            if text:
                turn.events.append("**Assistant:**\n\n" + demote_headings(text))
        elif block_type == "tool_use":
            name = str(block.get("name", "unknown"))
            turn.tool_counts[name] += 1
            if name == "AskUserQuestion" and block.get("id"):
                ask_tool_ids.add(block["id"])
            description = block.get("input", {}).get("description") if isinstance(block.get("input"), dict) else None
            if name == "Bash" and description:
                turn.bash_descriptions.append(str(description))


def handle_user(record: Record, turns: list[Turn], current_turn: Any, ask_tool_ids: set[str]) -> None:
    content = record.get("message", {}).get("content")
    if isinstance(content, str):
        text = strip_system_reminders(content)
        if text:
            turns.append(Turn(prompt=text))
        return
    for block in content_blocks(record):
        if block.get("type") == "tool_result" and block.get("tool_use_id") in ask_tool_ids:
            text = tool_result_text(block)
            if text:
                current_turn().events.append("**Answers:**\n\n" + render_quote(text))


def render_header(metadata: Metadata) -> str:
    return "\n".join([
        "# Session transcript",
        "",
        f"- Session id: {metadata.session_id}",
        f"- Transcript path: {metadata.path}",
        f"- Working directory: {metadata.cwd or 'unknown'}",
        f"- First timestamp: {metadata.first_timestamp or 'unknown'}",
        f"- Last timestamp: {metadata.last_timestamp or 'unknown'}",
    ])


def render_turn(number: int, turn: Turn) -> str:
    lines = [f"## Turn {number}", ""]
    if turn.prompt is not None:
        lines += ["**User:**", "", render_quote(turn.prompt), ""]
    for event in turn.events:
        lines += [event, ""]
    tools = summarize_tools(turn)
    if tools:
        lines += [tools, ""]
    return "\n".join(lines)


def render_markdown(metadata: Metadata, turns: list[Turn]) -> str:
    sections = [render_header(metadata)]
    sections += [render_turn(number, turn) for number, turn in enumerate(turns, start=1)]
    return "\n\n".join(sections).rstrip() + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--session-id", help="Session id; looked up as ~/.claude/projects/*/<id>.jsonl")
    parser.add_argument("--path", help="Path to a session .jsonl file")
    parser.add_argument("--out", help="Write Markdown here instead of stdout")
    args = parser.parse_args()

    try:
        path = resolve_transcript_path(args.session_id, args.path)
    except TranscriptError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1

    records = read_records(path)
    markdown = render_markdown(collect_metadata(records, path), build_turns(records))
    if args.out:
        out = Path(args.out).expanduser()
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(markdown, encoding="utf-8")
    else:
        sys.stdout.write(markdown)
    return 0


if __name__ == "__main__":
    sys.exit(main())

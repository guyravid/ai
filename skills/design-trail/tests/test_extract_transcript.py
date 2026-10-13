import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "scripts"))

import extract_transcript as et  # noqa: E402


def user_prompt(text: str, **extra: Any) -> dict[str, Any]:
    return {"type": "user", "message": {"role": "user", "content": text}, "timestamp": "2026-01-01T00:00:00Z", "cwd": "/work", **extra}


def assistant(blocks: list[dict[str, Any]], **extra: Any) -> dict[str, Any]:
    return {"type": "assistant", "message": {"role": "assistant", "content": blocks}, "timestamp": "2026-01-01T00:01:00Z", **extra}


def tool_use(tool_id: str, name: str, **input_fields: Any) -> dict[str, Any]:
    return {"type": "tool_use", "id": tool_id, "name": name, "input": input_fields}


def tool_result(tool_id: str, content: Any) -> dict[str, Any]:
    return {"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": tool_id, "content": content}]}}


class ExtractTranscriptTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.dir = Path(self.tmp.name)

    def render(self, records: list[Any], raw_extra: str = "") -> str:
        path = self.dir / "abc.jsonl"
        lines = [r if isinstance(r, str) else json.dumps(r) for r in records]
        path.write_text("\n".join(lines) + "\n" + raw_extra)
        loaded = et.read_records(path)
        return et.render_markdown(et.collect_metadata(loaded, path), et.build_turns(loaded))

    def test_human_prompt_assistant_text_and_header(self) -> None:
        out = self.render([user_prompt("hello there"), assistant([{"type": "text", "text": "hi back"}])])
        self.assertIn("- Session id: abc", out)
        self.assertIn("- Working directory: /work", out)
        self.assertIn("- First timestamp: 2026-01-01T00:00:00Z", out)
        self.assertIn("## Turn 1", out)
        self.assertIn("> hello there", out)
        self.assertIn("hi back", out)

    def test_assistant_headings_demoted_below_turns(self) -> None:
        text = "## Plan\n\n```bash\n# a comment\n```\n###### Deep"
        output = self.render([user_prompt("hi"), assistant([{"type": "text", "text": text}])])
        self.assertIn("#### Plan", output)
        self.assertNotIn("\n## Plan", output)
        self.assertIn("\n# a comment", output)
        self.assertIn("###### Deep", output)
        self.assertEqual(output.count("## Turn "), 1)

    def test_system_reminder_stripped(self) -> None:
        out = self.render([user_prompt("real ask <system-reminder>\nsecret\nstuff\n</system-reminder>")])
        self.assertIn("real ask", out)
        self.assertNotIn("secret", out)

    def test_mid_turn_message(self) -> None:
        queued = {"type": "attachment", "attachment": {"type": "queued_command", "prompt": "also this"}}
        out = self.render([user_prompt("start"), assistant([{"type": "text", "text": "working"}]), queued])
        self.assertIn("(sent mid-turn)", out)
        self.assertIn("> also this", out)
        self.assertEqual(out.count("## Turn"), 1)

    def test_other_attachments_dropped(self) -> None:
        other = {"type": "attachment", "attachment": {"type": "hook_success", "prompt": "noise"}}
        self.assertNotIn("noise", self.render([user_prompt("x"), other]))

    def test_ask_user_question_answer(self) -> None:
        out = self.render([
            user_prompt("decide"),
            assistant([tool_use("t1", "AskUserQuestion", questions=[])]),
            tool_result("t1", "The user answered: \"Q\"=\"A\""),
        ])
        self.assertIn("**Answers:**", out)
        self.assertIn("\"Q\"=\"A\"", out)

    def test_other_tool_results_dropped(self) -> None:
        out = self.render([
            user_prompt("go"),
            assistant([tool_use("t2", "Read", file_path="/x")]),
            tool_result("t2", "FILE CONTENTS"),
        ])
        self.assertNotIn("FILE CONTENTS", out)

    def test_answer_as_text_block_list(self) -> None:
        out = self.render([
            user_prompt("decide"),
            assistant([tool_use("t1", "AskUserQuestion")]),
            tool_result("t1", [{"type": "text", "text": "listed answer"}]),
        ])
        self.assertIn("listed answer", out)

    def test_thinking_sidechain_meta_dropped(self) -> None:
        out = self.render([
            user_prompt("keep me"),
            assistant([{"type": "thinking", "thinking": "private musings"}, {"type": "text", "text": "visible"}]),
            assistant([{"type": "text", "text": "subagent chatter"}], isSidechain=True),
            user_prompt("meta prompt", isMeta=True),
            user_prompt("sidechain prompt", isSidechain=True),
        ])
        self.assertIn("visible", out)
        for dropped in ("private musings", "subagent chatter", "meta prompt", "sidechain prompt"):
            self.assertNotIn(dropped, out)
        self.assertEqual(out.count("## Turn"), 1)

    def test_tool_summary_line(self) -> None:
        out = self.render([
            user_prompt("work"),
            assistant([
                tool_use("a", "Bash", description="List repo layout"),
                tool_use("b", "Bash", command="ls"),
                tool_use("c", "Bash", description="Run tests"),
                tool_use("d", "Read", file_path="/x"),
            ]),
        ])
        self.assertIn("Tools: Bash×3 (List repo layout; Run tests), Read×1", out)

    def test_malformed_lines_skipped(self) -> None:
        out = self.render([user_prompt("ok"), "{not json"], raw_extra="[1,2]\n")
        self.assertIn("> ok", out)

    def test_empty_prompt_after_stripping_skipped(self) -> None:
        out = self.render([user_prompt("<system-reminder>only</system-reminder>")])
        self.assertNotIn("## Turn", out)

    def test_resolve_by_session_id(self) -> None:
        project = self.dir / "proj"
        project.mkdir()
        (project / "sid1.jsonl").write_text("{}\n")
        self.assertEqual(et.resolve_transcript_path("sid1", None, self.dir), project / "sid1.jsonl")

    def test_resolve_missing_session(self) -> None:
        with self.assertRaises(et.TranscriptError):
            et.resolve_transcript_path("nope", None, self.dir)

    def test_resolve_ambiguous_session(self) -> None:
        for name in ("p1", "p2"):
            (self.dir / name).mkdir()
            (self.dir / name / "dup.jsonl").write_text("{}\n")
        with self.assertRaisesRegex(et.TranscriptError, "ambiguous"):
            et.resolve_transcript_path("dup", None, self.dir)

    def test_resolve_path_and_missing_path(self) -> None:
        file = self.dir / "f.jsonl"
        file.write_text("{}\n")
        self.assertEqual(et.resolve_transcript_path(None, str(file), self.dir), file)
        with self.assertRaises(et.TranscriptError):
            et.resolve_transcript_path(None, str(self.dir / "missing.jsonl"), self.dir)

    def test_resolve_env_fallback_and_no_args(self) -> None:
        project = self.dir / "proj"
        project.mkdir()
        (project / "envid.jsonl").write_text("{}\n")
        original = os.environ.pop("CLAUDE_CODE_SESSION_ID", None)
        self.addCleanup(lambda: os.environ.pop("CLAUDE_CODE_SESSION_ID", None))
        try:
            with self.assertRaises(et.TranscriptError):
                et.resolve_transcript_path(None, None, self.dir)
            os.environ["CLAUDE_CODE_SESSION_ID"] = "envid"
            self.assertEqual(et.resolve_transcript_path(None, None, self.dir), project / "envid.jsonl")
        finally:
            if original is not None:
                os.environ["CLAUDE_CODE_SESSION_ID"] = original


if __name__ == "__main__":
    unittest.main()

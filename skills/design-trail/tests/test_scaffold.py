import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent.parent / "scripts"
sys.path.insert(0, str(SCRIPTS))

import scaffold  # noqa: E402


class ScaffoldTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_creates_layout(self) -> None:
        result = scaffold.scaffold(self.root, "Proj")
        for directory in ("docs/00-origin", "docs/decisions", "docs/research"):
            self.assertTrue((self.root / directory).is_dir())
        for file in scaffold.SKELETON_FILES:
            self.assertTrue((self.root / file).is_file())
            self.assertIn(file, result["created"])
        self.assertEqual(result["existing"], [])

    def test_title_substitution(self) -> None:
        scaffold.scaffold(self.root, "Agent platform")
        for file in scaffold.SKELETON_FILES:
            self.assertNotIn("{{TITLE}}", (self.root / file).read_text())
        self.assertIn("Agent platform", (self.root / "docs/README.md").read_text())

    def test_never_overwrites_and_reports_existing(self) -> None:
        scaffold.scaffold(self.root, "Proj")
        readme = self.root / "docs/README.md"
        readme.write_text("custom")
        result = scaffold.scaffold(self.root, "Other")
        self.assertEqual(readme.read_text(), "custom")
        self.assertEqual(result["created"], [])
        self.assertIn("docs/README.md", result["existing"])
        self.assertIn("docs/decisions/", result["existing"])

    def test_cli_missing_root_errors(self) -> None:
        missing = self.root / "nope"
        proc = subprocess.run(
            [sys.executable, str(SCRIPTS / "scaffold.py"), "--root", str(missing)],
            capture_output=True, text=True,
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("root does not exist", json.loads(proc.stdout)["error"])
        self.assertFalse(missing.exists())

    def test_never_touches_claude_md(self) -> None:
        scaffold.scaffold(self.root, "Proj")
        self.assertFalse((self.root / "CLAUDE.md").exists())


if __name__ == "__main__":
    unittest.main()

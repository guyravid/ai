#!/usr/bin/env python3
"""Create the design-trail docs/ layout under a repo root without overwriting anything."""

import argparse
import json
import sys
from pathlib import Path

TEMPLATES_DIR = Path(__file__).resolve().parent.parent / "assets" / "templates"

DIRECTORIES = ["docs", "docs/00-origin", "docs/decisions", "docs/research"]

# docs path -> template file name; decision.md and research.md are copied per record, not here.
SKELETON_FILES = {
    "docs/README.md": "README.md",
    "docs/01-thought-process.md": "thought-process.md",
    "docs/open-questions.md": "open-questions.md",
    "docs/00-origin/session.md": "session.md",
}


def scaffold(root: Path, title: str) -> dict[str, list[str]]:
    created: list[str] = []
    existing: list[str] = []

    for relative in DIRECTORIES:
        directory = root / relative
        if directory.is_dir():
            existing.append(relative + "/")
        else:
            directory.mkdir(parents=True)
            created.append(relative + "/")

    for relative, template_name in SKELETON_FILES.items():
        target = root / relative
        if target.exists():
            existing.append(relative)
            continue
        content = (TEMPLATES_DIR / template_name).read_text()
        target.write_text(content.replace("{{TITLE}}", title))
        created.append(relative)

    return {"created": created, "existing": existing}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, help="Repo root to create docs/ in; must exist")
    parser.add_argument("--title", default="Untitled project", help="Project or working title")
    args = parser.parse_args()

    root = Path(args.root).expanduser()
    if not root.is_dir():
        print(json.dumps({"error": f"root does not exist: {root}"}))
        return 1

    print(json.dumps({"root": str(root), **scaffold(root, args.title)}, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())

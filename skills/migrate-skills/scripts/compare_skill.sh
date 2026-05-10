#!/usr/bin/env bash
# Compare a source skill directory against an installed one.
# Usage: compare_skill.sh <source-dir> <target-dir>
# Exit 0 = identical, 1 = differences exist (diff output to stdout).
set -euo pipefail
diff -rq "$1" "$2"

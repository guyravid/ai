#!/usr/bin/env bash
# Compare a source AGENT.md against an installed agent file.
# Usage: compare_agent.sh <source-dir> <target-file>
# Exit 0 = identical, 1 = differences exist (diff output to stdout).
set -euo pipefail
diff "$1/AGENT.md" "$2"

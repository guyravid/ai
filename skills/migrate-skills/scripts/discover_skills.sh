#!/usr/bin/env bash
# Discover all skills under a source directory.
# Usage: discover_skills.sh <source-dir>
# Prints one skill directory path per line.
set -euo pipefail
find "$1" -name "SKILL.md" -type f | sed 's|/SKILL\.md$||' | sort

#!/usr/bin/env bash
# Discover all agents under a source directory.
# Usage: discover_agents.sh <source-dir>
# Prints one agent directory path per line.
set -euo pipefail
find "$1" -name "AGENT.md" -type f \
  ! -path "*/node_modules/*" \
  ! -path "*/.git/*" \
  ! -path "*/.venv/*" \
  | sed 's|/AGENT\.md$||' | sort

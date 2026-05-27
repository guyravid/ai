#!/usr/bin/env bash
# Discover candidate MCP server directories under a source directory.
# Usage: discover_mcps.sh <source-dir>
# Finds directories whose name contains "mcp" (case-insensitive),
# excluding noise directories. Prints one path per line.
# Results are presented to the user for confirmation — they are candidates, not guarantees.
set -euo pipefail
find "$1" -maxdepth 4 -type d -iname "*mcp*" \
  ! -path "*/node_modules/*" \
  ! -path "*/.git/*" \
  ! -path "*/.venv/*" \
  ! -path "*/dist/*" \
  | sort

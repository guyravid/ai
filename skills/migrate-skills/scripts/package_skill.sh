#!/usr/bin/env bash
# Package a skill directory as a .skill zip for manual cloud upload.
# Usage: package_skill.sh <skill-dir>
# Output: prints the path to the created .skill file.
set -euo pipefail
name="$(basename "$1")"
cd "$(dirname "$1")"
zip -r "/tmp/${name}.skill" "${name}/"
echo "/tmp/${name}.skill"

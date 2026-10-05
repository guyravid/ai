#!/usr/bin/env bash
# Drift check for the agent-CLI bundle: contract <-> conformance checklist <-> verifier.
#
#   check_checklist.sh
#
# Runs from any directory; paths are resolved relative to this script. Prints one line per problem
# and a summary. Exit 0 when clean, 1 when any check fails. Needs only bash, grep, sed, awk, sort.
#
# Files:  CHECKLIST = ../references/conformance-checklist.md
#         VERIFIER  = ./verify_conformance.sh
#         CONTRACT  = ../../../contract/CONTRACT.md
#         HISTORY   = ../../../contract/history/*.md
#
# Definitions
#   Check ID:     C<section>-<n> or C<section>.<sub>-<n>, e.g. C7-13, C8.4-3.
#   Defined:      a checklist line "- **<ID>** auto|manual ..." inside a "## §N" section. "auto" also
#                 covers "auto, conditional". Lines outside a "## §N" section (for example the
#                 "Highest-stakes failures" index, which cites IDs without defining them) are not
#                 definitions and are ignored by checks a, c and d.
#   Verifier use: an ok|bad|warn|skip "<ID>" call, or an ID listed in a *_IDS="..." variable (the
#                 verifier skips those in a loop). Other mentions (comments, messages) do not count.
#
# Checks
#   a. Every auto ID has at least one verifier use (a skip call counts: conditional checks skip).
#   b. Every ID used by the verifier is defined in the checklist. A manual ID may appear in the
#      verifier only through skip calls (skip with an explanation). ok, bad or warn on a manual ID
#      is an error, because the checklist says the verifier does not cover it.
#   c. No ID is defined twice among the definitions.
#   d. An ID's section matches its location: under "## §N" the ID is C<N>-n or C<N>.<m>-n; under a
#      "### §N.M" sub-heading it must be exactly C<N>.<M>-n. A definition line that does not parse,
#      or sits before any "## §N" heading, is an error.
#   e. Every §N / §N.M in the checklist and verifier matches a "## §N" or "### §N.M" heading in
#      CONTRACT.md. The check is by heading number only, not by title.
#   f. The checklist's "Last updated for contract X" header (first "updated for contract X" in the file)
#      equals the version in CONTRACT.md's "**Contract version `X`**" line.
#   g. The history file that sorts last by name (YYYY-MM-DD-NN-*.md, so newest date then highest
#      sequence) has frontmatter contract_version equal to the contract header version.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECKLIST="$HERE/../references/conformance-checklist.md"
VERIFIER="$HERE/verify_conformance.sh"
CONTRACT="$HERE/../../../contract/CONTRACT.md"
HISTORY_DIR="$HERE/../../../contract/history"

for f in "$CHECKLIST" "$VERIFIER" "$CONTRACT"; do
  [ -r "$f" ] || { echo "FAIL  missing input: $f"; exit 1; }
done

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
PROBLEMS=0
problem() { printf 'FAIL  [%s] %s\n' "$1" "$2"; PROBLEMS=$((PROBLEMS+1)); }

ID_RE='C[0-9]+(\.[0-9]+)?-[0-9]+'

# Checklist definitions -> "ID kind line", plus structural problems -> "line message".
awk -v id_re="^$ID_RE\$" '
  /^## §[0-9]+/   { h2 = $2; sub(/^§/, "", h2); h3 = ""; insec = 1; next }
  /^### §[0-9]/   { h3 = $2; sub(/^§/, "", h3); next }
  /^## /          { insec = 0; h2 = ""; h3 = ""; next }
  /^- \*\*C[0-9]/ {
    if (!insec) next
    if (match($0, /^- \*\*C[0-9]+(\.[0-9]+)?-[0-9]+\*\* (auto|manual)/) == 0) {
      print "BAD " NR " malformed definition line: " substr($0, 1, 60); next
    }
    id = $2; gsub(/\*/, "", id)
    kind = $3
    sec = id; sub(/^C/, "", sec); sub(/-[0-9]+$/, "", sec)
    ok = (h3 != "") ? (sec == h3) : (sec == h2 || index(sec, h2 ".") == 1)
    if (!ok) print "SEC " NR " " id " is under §" (h3 != "" ? h3 : h2) " but names §" sec
    print "DEF " id " " kind " " NR
  }
' "$CHECKLIST" >"$WORK/checklist.raw"

grep '^DEF ' "$WORK/checklist.raw" | awk '{k=$3; sub(/,$/,"",k); print $2, k, $4}' >"$WORK/defs.txt"
cut -d' ' -f1 "$WORK/defs.txt" | sort -u >"$WORK/def_ids.txt"
awk '$2=="auto"{print $1}' "$WORK/defs.txt" | sort -u >"$WORK/auto_ids.txt"
awk '$2=="manual"{print $1}' "$WORK/defs.txt" | sort -u >"$WORK/manual_ids.txt"

# Verifier uses -> "ID verb line".
awk -v id_re="$ID_RE" '
  {
    line = $0
    if (match(line, /(^|[^A-Za-z_])(ok|bad|warn|skip)[ \t]+"C[0-9]+(\.[0-9]+)?-[0-9]+"/)) {
      s = substr(line, RSTART, RLENGTH)
      verb = s; sub(/^[^a-z]*/, "", verb); sub(/[ \t].*$/, "", verb)
      id = s; sub(/^[^"]*"/, "", id); sub(/"$/, "", id)
      print id, verb, NR
    }
    if (match(line, /^[ \t]*[A-Za-z_]*_IDS="[^"]*"/)) {
      s = substr(line, RSTART, RLENGTH); sub(/^[^"]*"/, "", s); sub(/"$/, "", s)
      n = split(s, parts, /[ \t]+/)
      for (i = 1; i <= n; i++) if (parts[i] ~ ("^" id_re "$")) print parts[i], "skip", NR
    }
  }
' "$VERIFIER" >"$WORK/uses.txt"
cut -d' ' -f1 "$WORK/uses.txt" | sort -u >"$WORK/used_ids.txt"

# a. auto IDs with no verifier use
while read -r id; do
  [ -n "$id" ] && problem a "$id is marked auto in the checklist but verify_conformance.sh never calls ok|bad|warn|skip for it"
done < <(comm -23 "$WORK/auto_ids.txt" "$WORK/used_ids.txt")

# b. verifier IDs unknown to the checklist, or manual and not skip-only
while read -r id; do
  [ -n "$id" ] && problem b "$id is used by verify_conformance.sh (line $(awk -v i="$id" '$1==i{print $3; exit}' "$WORK/uses.txt")) but not defined in the checklist"
done < <(comm -13 "$WORK/def_ids.txt" "$WORK/used_ids.txt")
while read -r id; do
  [ -z "$id" ] && continue
  while read -r line verb; do
    problem b "$id is marked manual in the checklist but verify_conformance.sh line $line calls $verb for it (only skip is allowed)"
  done < <(awk -v i="$id" '$1==i && $2!="skip"{print $3, $2}' "$WORK/uses.txt")
done < <(comm -12 "$WORK/manual_ids.txt" "$WORK/used_ids.txt")

# c. duplicate definitions
while read -r id; do
  lines=$(awk -v i="$id" '$1==i{printf "%s ", $3}' "$WORK/defs.txt")
  problem c "$id is defined more than once in the checklist (lines ${lines% })"
done < <(cut -d' ' -f1 "$WORK/defs.txt" | sort | uniq -d)

# d. section number vs location, malformed definitions
while read -r _ line rest; do
  problem d "checklist line $line: $rest"
done < <(grep -E '^(SEC|BAD) ' "$WORK/checklist.raw")

# e. § references against CONTRACT headings
grep -E '^#{2,3} §[0-9]' "$CONTRACT" | sed -E 's/^#+ §([0-9]+(\.[0-9]+)*).*/\1/' | sort -u >"$WORK/headings.txt"
for file in "$CHECKLIST" "$VERIFIER"; do
  while IFS=: read -r line ref; do
    num="${ref#§}"
    grep -qx -- "$num" "$WORK/headings.txt" ||
      problem e "$(basename "$file") line $line cites $ref, which is not a heading in CONTRACT.md"
  done < <(grep -n -o -E '§[0-9]+(\.[0-9]+)*' "$file")
done

# f. checklist version header vs contract
contract_version=$(sed -n -E 's/^\*\*Contract version `([^`]+)`\*\*.*/\1/p' "$CONTRACT" | head -1)
checklist_version=$(grep -m1 -o -E 'updated for contract [0-9]+(\.[0-9]+)+' "$CHECKLIST" | sed 's/updated for contract //')
if [ -z "$contract_version" ]; then
  problem f "CONTRACT.md has no \"**Contract version \`X\`**\" header line"
elif [ -z "$checklist_version" ]; then
  problem f "the checklist has no \"Last updated for contract X\" header"
elif [ "$checklist_version" != "$contract_version" ]; then
  problem f "the checklist says it was last updated for contract $checklist_version but CONTRACT.md is $contract_version"
fi

# g. newest history entry vs contract
newest=$(ls "$HISTORY_DIR"/*.md 2>/dev/null | sort | tail -1)
if [ -z "$newest" ]; then
  problem g "no history files in $HISTORY_DIR"
else
  history_version=$(awk 'NR==1 && $0!="---"{exit} /^---$/{n++; next} n==1 && /^contract_version:/{v=$2; gsub(/["'"'"']/,"",v); print v; exit}' "$newest")
  if [ -z "$history_version" ]; then
    problem g "$(basename "$newest") has no contract_version in its frontmatter"
  elif [ "$history_version" != "$contract_version" ]; then
    problem g "newest history entry $(basename "$newest") names contract_version $history_version but CONTRACT.md is $contract_version"
  fi
fi

echo
echo "checklist: $(wc -l <"$WORK/def_ids.txt" | tr -d ' ') IDs ($(wc -l <"$WORK/auto_ids.txt" | tr -d ' ') auto), verifier: $(wc -l <"$WORK/used_ids.txt" | tr -d ' ') IDs, contract ${contract_version:-?}"
if [ "$PROBLEMS" -eq 0 ]; then
  echo "check_checklist: clean"
  exit 0
fi
echo "check_checklist: $PROBLEMS problem(s)"
exit 1

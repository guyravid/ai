#!/usr/bin/env bash
# Automated checks from references/conformance-checklist.md, run against a built binary.
#
#   verify_conformance.sh <binary> [ENV_PREFIX]
#
# Runs with nothing configured, no network access needed, and a clean environment: the real user's
# config files and variables are never visible to the binary under test. Items marked "manual" in
# the checklist need a fake upstream or a second build and are not covered here.
#
# Exit 0 when no check fails. Warnings (SHOULD-level budgets) do not fail the run.

set -uo pipefail

BIN="${1:?usage: verify_conformance.sh <binary> [ENV_PREFIX]}"
command -v jq >/dev/null || { echo "jq is required"; exit 1; }
[ -x "$BIN" ] || { echo "not executable: $BIN"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/home" "$WORK/xdg" "$WORK/cache" "$WORK/out"

PASS=0; FAIL=0; SKIP=0; WARN=0
ok()   { printf '  PASS  %-8s %s\n' "$1" "$2"; PASS=$((PASS+1)); }
bad()  { printf '  FAIL  %-8s %s\n' "$1" "$2"; FAIL=$((FAIL+1)); }
skip() { printf '  SKIP  %-8s %s\n' "$1" "$2"; SKIP=$((SKIP+1)); }
warn() { printf '  WARN  %-8s %s\n' "$1" "$2"; WARN=$((WARN+1)); }
section() { printf '\n%s\n' "$1"; }

# run NAME [VAR=value ...] -- ARGS...
# Runs the binary in a clean environment. Stdout -> $WORK/out/NAME.json, stderr -> NAME.err,
# exit status -> NAME.rc. Every output is kept so the ANSI and error-pairing checks can scan it.
run() {
  local name="$1"; shift
  local vars=()
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do vars+=("$1"); shift; done
  shift
  env -i PATH="$PATH" HOME="$WORK/home" XDG_CONFIG_HOME="$WORK/xdg" XDG_CACHE_HOME="$WORK/cache" \
      ${vars[@]+"${vars[@]}"} "$BIN" "$@" \
      >"$WORK/out/$name.json" 2>"$WORK/out/$name.err" </dev/null
  echo $? >"$WORK/out/$name.rc"
}
out() { cat "$WORK/out/$1.json"; }
rc()  { cat "$WORK/out/$1.rc"; }
is_one_json() { [ "$(jq -s 'length' "$WORK/out/$1.json" 2>/dev/null)" = "1" ]; }

# ---------------------------------------------------------------------------- baseline

run tools -- tools
if ! is_one_json tools || [ "$(rc tools)" != "0" ]; then
  echo "Conformance: $BIN"
  bad "C2-1" "\`tools\` did not return one JSON envelope with exit 0; nothing else can be checked"
  head -c 300 "$WORK/out/tools.json" | cat -v; echo
  printf '\npassed %d   failed %d   warnings %d   skipped %d\n' "$PASS" "$FAIL" "$WARN" "$SKIP"
  exit 1
fi
TOOL=$(jq -r '.tool' "$WORK/out/tools.json")
P="${2:-$(printf '%s' "$TOOL" | tr '[:lower:]-' '[:upper:]_')}"
echo "Conformance: $BIN (tool '$TOOL', prefix ${P}_)"

run describe -- describe
run detail   -- tools --detail
run lc       -- list-config
run teach    -- teach
run lp       -- list-profiles

has_cmd() { jq -e --arg n "$1" '.data | index($n)' "$WORK/out/tools.json" >/dev/null; }

# ---------------------------------------------------------------------------- §2 streams

section "§2 Output streams"

CHECK_CMDS=("tools" "tools --detail" "describe" "list-config" "version")
all_single=1
for c in "${CHECK_CMDS[@]}"; do
  # shellcheck disable=SC2086
  run s2 -- $c
  if ! is_one_json s2 || [ "$(tail -c1 "$WORK/out/s2.json" | od -An -c | tr -d ' ')" != '\n' ]; then
    all_single=0; bad "C2-1" "\`$c\` did not write exactly one JSON document and a newline"
  fi
done
[ $all_single = 1 ] && ok "C2-1" "one JSON document per invocation"

run unknown -- no-such-command
if is_one_json unknown && jq -e '.ok == false' "$WORK/out/unknown.json" >/dev/null 2>&1; then
  ok "C2-2" "error envelope written to stdout"
else
  bad "C2-2" "unknown command produced no error envelope on stdout"
fi

# C2-4: identical output under a terminal, where one can be allocated.
if command -v script >/dev/null 2>&1 &&
   env -i PATH="$PATH" HOME="$WORK/home" XDG_CONFIG_HOME="$WORK/xdg" XDG_CACHE_HOME="$WORK/cache" \
       script -q "$WORK/tty.raw" "$BIN" tools </dev/null >/dev/null 2>&1 && [ -s "$WORK/tty.raw" ]; then
  TTYJSON=$(sed -e 's/[^[:print:]]//g' "$WORK/tty.raw" | grep -o '{.*' | head -1)
  if [ "$(printf '%s' "$TTYJSON" | jq -S -c . 2>/dev/null)" = "$(jq -S -c . "$WORK/out/tools.json")" ]; then
    ok "C2-4" "identical output with a terminal attached"
  else
    bad "C2-4" "output differs when a terminal is attached"
  fi
else
  skip "C2-4" "no pseudo-terminal available here"
fi

# C2-5: stdin held open and never written must not block the tool.
mkfifo "$WORK/fifo"
exec 3<>"$WORK/fifo"
env -i PATH="$PATH" HOME="$WORK/home" XDG_CONFIG_HOME="$WORK/xdg" XDG_CACHE_HOME="$WORK/cache" \
    "$BIN" tools <"$WORK/fifo" >"$WORK/stdin.json" 2>/dev/null &
SPID=$!
for _ in $(seq 1 50); do kill -0 $SPID 2>/dev/null || break; sleep 0.1; done
if kill -0 $SPID 2>/dev/null; then
  kill $SPID 2>/dev/null; bad "C2-5" "\`tools\` blocked on an open stdin"
else
  ok "C2-5" "does not block on stdin"
fi
exec 3>&-

# ---------------------------------------------------------------------------- §3 envelope

section "§3 The envelope"

if jq -e 'keys == ["command","data","meta","ok","tool"]' "$WORK/out/tools.json" >/dev/null; then
  ok "C3-1" "success envelope has exactly ok, tool, command, data, meta"
else
  bad "C3-1" "success top-level members: $(jq -c 'keys' "$WORK/out/tools.json")"
fi

if jq -e '(.meta.contract_version | test("^[0-9]+\\.[0-9]+(\\.[0-9]+)?$")) and (.meta.tool_version | type == "string")' \
     "$WORK/out/tools.json" >/dev/null 2>&1; then
  ok "C3-2" "meta carries contract_version (major.minor[.patch]) and tool_version"
else
  bad "C3-2" "meta.contract_version or meta.tool_version missing or malformed"
fi

if jq -e '.meta | has("timing") | not' "$WORK/out/tools.json" >/dev/null; then
  ok "C3-3" "no timing in meta without --timing"
else
  bad "C3-3" "meta.timing present without --timing"
fi

URC=$(rc unknown)
if jq -e --argjson rc "$URC" '(.error | has("code") and has("exit_code") and has("message") and has("retriable"))
      and .error.exit_code == $rc and .data == null' "$WORK/out/unknown.json" >/dev/null 2>&1; then
  ok "C3-4" "error envelope complete; exit_code equals the exit status; data is null"
else
  bad "C3-4" "error envelope incomplete, or exit_code differs from exit status $URC"
fi

if jq -e '(.error.message | length <= 512) and ((.error.details // {}) | tojson | length <= 2048)' \
     "$WORK/out/unknown.json" >/dev/null 2>&1; then
  ok "C3-5" "message and details within size limits"
else
  bad "C3-5" "error message over 512 characters or details over 2048 bytes"
fi

is_one_json unknown && ok "C3-6" "usage error prints no help text" \
                    || bad "C3-6" "usage error output is not a single JSON document"

if jq -e '.error.details | tojson | contains("no-such-command")' "$WORK/out/unknown.json" >/dev/null 2>&1; then
  ok "C3-7" "unknown command is named in details"
else
  bad "C3-7" "error details do not name the unknown command"
fi

# ---------------------------------------------------------------------------- §5 naming

section "§5 Command naming"

RESERVED='^(tools|describe|teach|doctor|list-config|list-profiles|dataset|serve|version|write|help)$'
CONTRACT_CMDS='^(tools|describe|teach|doctor|list-config|list-profiles|version|serve|dataset\.(read|list|stat|rm|clear))$'

if jq -e --arg c "$CONTRACT_CMDS" '[.data[] | select(test($c) | not)
      | test("^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)+$")] | all' "$WORK/out/tools.json" >/dev/null; then
  ok "C5-1" "domain command names are dotted, lowercase, two or more segments"
else
  bad "C5-1" "a domain command name is malformed"
fi

if jq -e '[.data.commands[] | .argv == (.name | split("."))] | all' "$WORK/out/describe.json" >/dev/null 2>&1; then
  ok "C5-2" "argv equals the name split on dots"
else
  bad "C5-2" "a describe entry's argv does not match its name"
fi

# C5-3: readOnlyHint is the opposite of mutates, and the optional write prefix is only on mutating commands.
if jq -e '[.data[] | (.mutates | type == "boolean") and ((.name | startswith("write.") | not) or .mutates)] | all' \
     "$WORK/out/detail.json" >/dev/null 2>&1 &&
   jq -e --slurpfile d "$WORK/out/detail.json" '($d[0].data | map({(.name): .mutates}) | add) as $m
      | [.data.commands[] | .annotations.readOnlyHint == ($m[.name] | not)] | all' \
     "$WORK/out/describe.json" >/dev/null 2>&1; then
  ok "C5-3" "readOnlyHint is the opposite of mutates; write-prefixed names are mutating"
else
  bad "C5-3" "readOnlyHint and mutates disagree, or a write-prefixed command is not mutating"
fi

if jq -e --arg c "$CONTRACT_CMDS" --arg r "$RESERVED" '[.data[] | select(test($c) | not)
      | select(startswith("write.") | not) | split(".")[0] | test($r) | not] | all' \
     "$WORK/out/tools.json" >/dev/null; then
  ok "C5-4" "no domain group uses a reserved name"
else
  bad "C5-4" "a domain command starts with a reserved name"
fi

# ---------------------------------------------------------------------------- §6 discovery

section "§6 Discovery"

nc_ok=1
for n in tools detail describe teach lc lp; do [ "$(rc $n)" = "0" ] || { nc_ok=0; bad "C6-1" "\`$n\` exited $(rc $n) with nothing configured"; }; done
[ $nc_ok = 1 ] && ok "C6-1" "discovery, teach, list-config, and list-profiles work with nothing configured"

if jq -e '(.data | type == "array") and ([.data[] | type == "string"] | all) and (.data == (.data | sort))' \
     "$WORK/out/tools.json" >/dev/null; then
  ok "C6-2" "tools is a sorted flat array of names"
else
  bad "C6-2" "tools is not a sorted flat array of strings"
fi

if jq -e '[.data[] | (keys == ["description","mutates","name"])
      and (.description | length <= 160) and (.description | contains("\n") | not)] | all' \
     "$WORK/out/detail.json" >/dev/null 2>&1; then
  ok "C6-3" "tools --detail entries are {name, description, mutates}, one line, ≤160 chars"
else
  bad "C6-3" "a tools --detail entry has the wrong members or an over-long description"
fi

FIRST=$(jq -r '.data[0]' "$WORK/out/tools.json")
run desc1 -- describe "$FIRST"
run descx -- describe no.such.command
if [ "$(jq '.data.commands | length' "$WORK/out/desc1.json" 2>/dev/null)" = "1" ] && [ "$(rc descx)" = "2" ]; then
  ok "C6-4" "describe <name> returns one command; an unknown name is usage"
else
  bad "C6-4" "describe <name> did not narrow to one command, or an unknown name was not usage"
fi

if jq -e '[.data.commands[] | has("argv") and has("inputSchema") and has("outputSchema")
      and (.annotations.readOnlyHint | type == "boolean") and has("x-cli")] | all' \
     "$WORK/out/describe.json" >/dev/null 2>&1; then
  ok "C6-5" "every describe entry has argv, schemas, readOnlyHint, x-cli"
else
  bad "C6-5" "a describe entry is missing a required member"
fi

if [ "$(jq '[paths | select(.[-1] == "envelope_schema")] | length' "$WORK/out/describe.json")" = "1" ]; then
  ok "C6-6" "envelope_schema appears exactly once"
else
  bad "C6-6" "envelope_schema is missing or repeated"
fi

# Shaping (--fields, empty-stripping, size caps) removes and replaces values, so outputSchema stays open.
strict_schemas=$(jq '[.data.commands[] | .name as $n | .outputSchema | .. | objects
    | select(((.required | type) == "array")
          or (.additionalProperties == false)
          or (((.type == "string") or ((.type | type) == "array" and (.type | index("string"))))
              and (has("maxLength") or has("pattern") or has("enum") or has("const"))))
    | $n] | unique' "$WORK/out/describe.json" 2>/dev/null)
if [ "$strict_schemas" = "[]" ]; then
  ok "C6-17" "no outputSchema requires properties, closes objects, or constrains strings"
else
  bad "C6-17" "outputSchema forbids shaped output (required, additionalProperties:false, or string constraints): $(echo "$strict_schemas" | jq -c .)"
fi

run capped -- tools --max-bytes 1
if [ "$(jq -c '.data' "$WORK/out/capped.json" 2>/dev/null)" = "$(jq -c '.data' "$WORK/out/tools.json")" ]; then
  ok "C6-7" "discovery output is not truncated by --max-bytes"
else
  bad "C6-7" "tools output changed under --max-bytes 1"
fi

TEACHF="$WORK/out/teach.json"
if [ "$(head -c1 "$TEACHF")" != "{" ] && [ "$(rc teach)" = "0" ] && grep -q "teach contract" "$TEACHF"; then
  ok "C6-8" "bare teach is Markdown and points to teach contract"
else
  bad "C6-8" "bare teach is not Markdown, failed, or does not name teach contract"
fi

TSIZE=$(wc -c <"$TEACHF" | tr -d ' ')
[ "$TSIZE" -lt 4096 ] && ok "C6-9" "bare teach is $TSIZE bytes" \
                      || warn "C6-9" "bare teach is $TSIZE bytes; the budget is 4096"

DSIZE=$(wc -c <"$WORK/out/detail.json" | tr -d ' ')
[ "$DSIZE" -lt 8192 ] && ok "C6-19" "tools --detail is $DSIZE bytes" \
                      || warn "C6-19" "tools --detail is $DSIZE bytes; the budget is 8192"

topics_ok=1
for t in "contract" "flags" "config" "--list"; do
  run "teach_${t#--}" -- teach "$t"
  f="$WORK/out/teach_${t#--}.json"
  if [ "$(rc "teach_${t#--}")" != "0" ] || [ ! -s "$f" ] || [ "$(head -c1 "$f")" = "{" ]; then
    topics_ok=0; bad "C6-10" "\`teach $t\` failed or returned JSON"
  fi
done
[ $topics_ok = 1 ] && ok "C6-10" "teach contract, flags, config, and --list return Markdown"

missing=""
while read -r s; do
  [ -n "$s" ] && ! grep -qi -- "$s" "$WORK/out/teach_config.json" && missing="$missing $s"
done < <(jq -r '.data.settings[].name' "$WORK/out/lc.json" 2>/dev/null)
[ -z "$missing" ] && ok "C6-11" "teach config names every setting" \
                  || bad "C6-11" "teach config omits:$missing"

# C6-12: every command line in backticks ("`<tool> word word …`") must resolve to a command
# in tools. Only backtick spans count: tool names are often ordinary words that appear in prose.
ghosts=""
for f in "$WORK"/out/teach*.json; do
  while read -r phrase; do
    words=($phrase); found=0
    for ((k=${#words[@]}; k>=1; k--)); do
      name=$(IFS=.; echo "${words[*]:0:k}")
      has_cmd "$name" && { found=1; break; }
    done
    [ $found = 0 ] && ghosts="$ghosts [$phrase]"
  done < <(grep -oE '`[^`]+`' "$f" | tr -d '`' | grep -E "^$TOOL( [a-z][a-z0-9-]*)+" \
             | grep -oE "^$TOOL( [a-z][a-z0-9-]*)+" | sed "s/^$TOOL //" | sort -u)
done
[ -z "$ghosts" ] && ok "C6-12" "every command named in teach exists" \
                 || bad "C6-12" "teach names commands that do not exist:$ghosts"

run tbad -- teach no-such-topic
[ "$(rc tbad)" = "2" ] && jq -e '.error.code == "usage"' "$WORK/out/tbad.json" >/dev/null 2>&1 \
  && ok "C6-13" "unknown teach topic is usage" || bad "C6-13" "unknown teach topic was not a usage error"

run help -- --help
if [ "$(rc help)" = "0" ] && tail -n1 "$WORK/out/help.json" | grep -q "^# machine-readable: $TOOL describe"; then
  ok "C6-14" "--help ends with the machine-readable pointer"
else
  bad "C6-14" "--help failed or its last line is not the describe pointer"
fi

# ---------------------------------------------------------------------------- §7 diagnostics

section "§7 Diagnostics"

run doctor -- doctor
if jq -e '[.data.checks[].status | IN("pass","warn","fail","skip")] | all' "$WORK/out/doctor.json" >/dev/null 2>&1 &&
   jq -e '[.data.checks[].name] as $n | ["config","credentials","network","auth","clock","datasets","writes"]
          | map(. as $c | $n | index($c)) | (all(. != null)) and (. == sort)' "$WORK/out/doctor.json" >/dev/null 2>&1; then
  ok "C7-1" "doctor statuses valid; required checks present and in order"
else
  bad "C7-1" "doctor has an invalid status, or a required check is missing or out of order"
fi

if [ "$(rc doctor)" != "0" ] &&
   jq -e '(.data.checks[] | select(.name == "credentials") | .status == "fail")
          and (.data.checks[] | select(.name == "auth") | .status == "skip")' "$WORK/out/doctor.json" >/dev/null 2>&1; then
  ok "C7-2" "doctor fails credentials when unconfigured and skips auth"
else
  bad "C7-2" "doctor did not fail credentials and skip auth when unconfigured (exit $(rc doctor))"
fi

REQUIRED_SETTINGS="CONFIG PROFILE LIMIT MAX_BYTES MAX_STRING MAX_DEPTH MAX_PAGES TIMEOUT BUDGET DATASET_DIR DATASET_TTL DATASET_MAX_BYTES DATASET_MAX_RECORDS DATASET_TOTAL_BYTES LOG_LEVEL"
missing=""
for s in $REQUIRED_SETTINGS; do
  jq -e --arg s "$s" '.data.settings[] | select(.name == $s) | has("value") and has("source") and has("origin") and has("default")' \
    "$WORK/out/lc.json" >/dev/null 2>&1 || missing="$missing $s"
done
[ -z "$missing" ] && ok "C7-4" "list-config reports every contract setting with full provenance" \
                  || bad "C7-4" "list-config missing or incomplete:$missing"

if jq -e '(.data.settings[] | select(.name == "LIMIT") | .origin == "builtin" and .source == "builtin")' \
     "$WORK/out/lc.json" >/dev/null 2>&1 &&
   jq -e '[.data.settings[] | select(.origin == "builtin") | .source == "builtin"] | all' "$WORK/out/lc.json" >/dev/null 2>&1; then
  ok "C7-5" "unset settings report origin and source builtin"
else
  bad "C7-5" "an unset setting does not report builtin"
fi

CRED=$(jq -r '[.data.settings[] | select(has("set")) | .name][0] // empty' "$WORK/out/lc.json")
if [ -n "$CRED" ] && jq -e '[.data.settings[] | select(has("set")) | .value == null and (.set | type == "boolean")] | all' \
     "$WORK/out/lc.json" >/dev/null 2>&1; then
  ok "C7-6" "credentials report value null and a boolean set"
else
  bad "C7-6" "no credential reported, or a credential exposes a value"
fi

run schema -- list-config --schema
CRED_LC=$(printf '%s' "$CRED" | tr '[:upper:]' '[:lower:]')
if jq -e '.data.schema | type == "object"' "$WORK/out/schema.json" >/dev/null 2>&1 &&
   ! jq -e --arg k "$CRED_LC" '[.. | objects | select(has("properties")) | .properties | has($k)] | any' \
       "$WORK/out/schema.json" >/dev/null 2>&1; then
  ok "C7-7" "config schema has no member that holds a credential value"
else
  bad "C7-7" "config schema missing, or it accepts '$CRED_LC' as a value"
fi

# ---------------------------------------------------------------------------- §10, §11

section "§10 Datasets and §11 Writes"

if has_cmd dataset.read; then
  run dsmiss -- dataset read 00000000-0000-0000-0000-000000000000
  if [ "$(rc dsmiss)" = "14" ] && jq -e '.error.code == "cache_miss" and (.error.hint | length > 0)' \
       "$WORK/out/dsmiss.json" >/dev/null 2>&1; then
    ok "C10-6" "unknown dataset is cache_miss, exit 14, with a hint"
  else
    bad "C10-6" "unknown dataset gave exit $(rc dsmiss) instead of cache_miss/14"
  fi
else
  skip "C10-6" "no dataset commands in this build"
fi

# C11-1: every mutating command, run without --confirm, must either be refused (exit 8) or fail as
# usage (exit 2: no request could be built, so nothing was sent). Dummy values follow the parameter's
# schema type: 1 for integer and number, the bare flag for boolean, "x" for anything else. At least one
# command must be refused with a preview object, which is the proof that the confirm gate sits before
# the request is sent. A refusal for another reason (a chat not allowed, say) counts as "not sent" only.
WRITES=$(jq -r '.data[] | select(.mutates) | .name' "$WORK/out/detail.json")
if [ -n "$WRITES" ]; then
  # Dummy credentials, so a tool cannot stop at "no credential" and hide a missing confirm check.
  DUMMY_VARS=()
  for v in $(jq -r '.data.auth.credentials[]?.env' "$WORK/out/describe.json"); do DUMMY_VARS+=("$v=dummy-not-a-real-key"); done
  c111_bad=""; c111_preview=0; c111_idx=0
  while IFS= read -r WRITE; do
    c111_idx=$((c111_idx+1))
    WARGS=()
    while IFS= read -r a; do WARGS+=("$a"); done < <(jq -r --arg n "$WRITE" '.data.commands[] | select(.name == $n) as $c
        | $c.argv[],
          ($c.inputSchema.required // [] | .[] as $p
           | ($c.inputSchema.properties[$p].type // "string") as $t
           | (if ($t | type) == "array" then ($t | map(select(. != "null")) | .[0] // "string") else $t end) as $ty
           | if ($c["x-cli"].positional // [] | index($p)) != null then (if $ty == "integer" or $ty == "number" then "1" else "x" end)
             else ($c["x-cli"].flags[$p] // ("--" + $p)),
                  (if $ty == "integer" or $ty == "number" then "1" elif $ty == "boolean" then empty else "x" end) end)' \
        "$WORK/out/describe.json")
    run "write$c111_idx" ${DUMMY_VARS[@]+"${DUMMY_VARS[@]}"} -- "${WARGS[@]}"
    wrc=$(rc "write$c111_idx"); wf="$WORK/out/write$c111_idx.json"
    if [ "$wrc" = "8" ] && jq -e '.error.code == "refused" and (.error.details.preview | type == "object")' "$wf" >/dev/null 2>&1; then
      c111_preview=$((c111_preview+1))
    elif [ "$wrc" = "8" ] && jq -e '.error.code == "refused" and ((.error.details.reason // "") | type == "string" and length > 0 and (test("confirm"; "i") | not))' "$wf" >/dev/null 2>&1; then
      :   # refused for another reason: nothing was sent, but this is not the preview proof
    elif [ "$wrc" = "2" ] && jq -e '.error.code == "usage"' "$wf" >/dev/null 2>&1; then
      :   # no request could be built
    else
      c111_bad="$c111_bad $WRITE(exit $wrc)"
    fi
  done <<<"$WRITES"
  if [ -n "$c111_bad" ]; then
    bad "C11-1" "write without --confirm was attempted or refused without a preview:$c111_bad"
  elif [ "$c111_preview" -eq 0 ]; then
    bad "C11-1" "no write command could be exercised; give at least one write a dummy-satisfiable argument set"
  else
    ok "C11-1" "every write without --confirm is refused or fails as usage; $c111_preview refused with a preview"
  fi
else
  skip "C11-1" "no write commands in this build"
fi

# ---------------------------------------------------------------------------- §12 secrets

section "§12 Secrets"

SECRETVAL="flagsecret${RANDOM}${RANDOM}x"
run sflag1 -- tools --api-key "$SECRETVAL"
run sflag2 -- tools "--token=$SECRETVAL"
sf_ok=1
for n in sflag1 sflag2; do
  if [ "$(rc $n)" != "8" ] || ! jq -e '.error.details.reason == "secret_on_argv"' "$WORK/out/$n.json" >/dev/null 2>&1 ||
     grep -q "$SECRETVAL" "$WORK/out/$n.json" "$WORK/out/$n.err"; then sf_ok=0; fi
done
[ $sf_ok = 1 ] && ok "C12-1" "credential flags refused with secret_on_argv; value not echoed" \
               || bad "C12-1" "a credential flag was accepted, misreported, or its value echoed"

CANARY="canary${RANDOM}${RANDOM}valueZZ"
CRED_ENVS=$(jq -r '.data.auth.credentials[]?.env' "$WORK/out/describe.json")
[ -z "$CRED_ENVS" ] && CRED_ENVS="${P}_${CRED}"
CANARY_VARS=()
for v in $CRED_ENVS; do CANARY_VARS+=("$v=$CANARY"); done
leak=""
for c in tools describe teach list-config list-profiles doctor; do
  # shellcheck disable=SC2086
  run "leak_$c" "${CANARY_VARS[@]}" -- $c
  grep -q "$CANARY" "$WORK/out/leak_$c.json" "$WORK/out/leak_$c.err" && leak="$leak $c"
done
[ -z "$leak" ] && ok "C12-2" "configured credential never appears in output" \
               || bad "C12-2" "credential value leaked by:$leak"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) skip "C12-3" "no POSIX permissions on this platform" ;;
  *)
    FIRST_ENV=$(printf '%s\n' $CRED_ENVS | head -1)
    printf 'permsecret\n' >"$WORK/loose.key"; chmod 644 "$WORK/loose.key"
    run perms "${FIRST_ENV}_FILE=$WORK/loose.key" -- doctor
    if [ "$(rc perms)" = "3" ]; then
      ok "C12-3" "world-readable credential file is a config error"
    else
      bad "C12-3" "world-readable credential file accepted (doctor exit $(rc perms), expected 3)"
    fi ;;
esac

# ---------------------------------------------------------------------------- §13 configuration

section "§13 Configuration"

lc_origin() { jq -r --arg s "$2" '.data.settings[] | select(.name == $s) | .origin' "$WORK/out/$1.json" 2>/dev/null; }

CFGDIR="$WORK/xdg/agentcli/$TOOL"
mkdir -p "$CFGDIR" "$WORK/cfg"

printf '{"default":{"limit":7}}' >"$CFGDIR/config.json"
run autoload -- list-config
if jq -e '.data.config_file != null' "$WORK/out/autoload.json" >/dev/null 2>&1 &&
   [ "$(lc_origin autoload LIMIT)" = "file_default" ]; then
  ok "C13-1" "config at the platform default location is loaded automatically"
  ok "C13-6" "a setting only in default resolves to file_default"
else
  bad "C13-1" "default-location config was not loaded"
  bad "C13-6" "LIMIT in default did not resolve to file_default (got '$(lc_origin autoload LIMIT)')"
fi

run envfile "${P}_LIMIT=9" -- list-config
[ "$(lc_origin envfile LIMIT)" = "env_default" ] && ok "C13-8" "environment outranks the config file" \
  || bad "C13-8" "file beat the environment (got '$(lc_origin envfile LIMIT)')"
rm -f "$CFGDIR/config.json"

run cfgmissing -- list-config --config "$WORK/cfg/absent.json"
[ "$(rc cfgmissing)" = "3" ] && ok "C13-2" "a named but missing config file is config" \
                             || bad "C13-2" "missing named config gave exit $(rc cfgmissing), expected 3"

printf '{ not json' >"$WORK/cfg/broken.json"
run cfgbroken -- list-config --config "$WORK/cfg/broken.json"
[ "$(rc cfgbroken)" = "3" ] && ok "C13-3" "unparseable config is config" \
                            || bad "C13-3" "unparseable config gave exit $(rc cfgbroken), expected 3"

printf '{"default":{"no_such_setting_xyz":1}}' >"$WORK/cfg/unknown.json"
run cfgunknown -- list-config --config "$WORK/cfg/unknown.json"
[ "$(rc cfgunknown)" = "3" ] && ok "C13-4" "unrecognised config member is config" \
                             || bad "C13-4" "unrecognised member gave exit $(rc cfgunknown), expected 3"

INLINE="inlinecred${RANDOM}${RANDOM}"
printf '{"default":{"%s":"%s"}}' "$CRED_LC" "$INLINE" >"$WORK/cfg/inline.json"
run cfginline -- list-config --config "$WORK/cfg/inline.json"
if [ "$(rc cfginline)" = "3" ] && ! grep -q "$INLINE" "$WORK/out/cfginline.json" "$WORK/out/cfginline.err"; then
  ok "C13-5" "inline credential in the config file is config, value not echoed"
else
  bad "C13-5" "inline credential gave exit $(rc cfginline) or the value was echoed"
fi

printf '{"default":{"limit":7},"profiles":{"probe":{"limit":8}}}' >"$WORK/cfg/prof.json"
run fileprof -- list-config --config "$WORK/cfg/prof.json" --profile probe
[ "$(lc_origin fileprof LIMIT)" = "file_profile" ] && ok "C13-7" "profile section outranks default section" \
  || bad "C13-7" "profiles.probe.limit did not resolve to file_profile (got '$(lc_origin fileprof LIMIT)')"

run envprof "${P}_LIMIT_PROBE=5" "${P}_LIMIT=9" -- list-config --profile probe
[ "$(lc_origin envprof LIMIT)" = "env_profile" ] && ok "C13-9" "profile-scoped variable resolves to env_profile" \
  || bad "C13-9" "${P}_LIMIT_PROBE did not win (got '$(lc_origin envprof LIMIT)')"

run inherit "${P}_TIMEOUT_PROBE=5s" "${P}_LIMIT=9" -- list-config --profile probe
[ "$(lc_origin inherit LIMIT)" = "env_default" ] && ok "C13-10" "a profile leaves unset settings at lower tiers" \
  || bad "C13-10" "selecting a profile reset LIMIT (got '$(lc_origin inherit LIMIT)', expected env_default)"

run family "AGENTCLI_LIMIT=11" -- list-config
[ "$(lc_origin family LIMIT)" = "family" ] && ok "C13-11" "AGENTCLI_ variable resolves to family" \
  || bad "C13-11" "AGENTCLI_LIMIT did not resolve to family (got '$(lc_origin family LIMIT)')"

run profenv "${P}_PROFILE=probe" "${P}_LIMIT_PROBE=5" "${P}_LIMIT_PROBE2=6" -- list-config
run profflag "${P}_PROFILE=probe" "${P}_LIMIT_PROBE=5" "${P}_LIMIT_PROBE2=6" -- list-config --profile probe2
if jq -e '.data.profile == "probe"' "$WORK/out/profenv.json" >/dev/null 2>&1 &&
   jq -e '.data.profile == "probe2"' "$WORK/out/profflag.json" >/dev/null 2>&1; then
  ok "C13-12" "${P}_PROFILE selects a profile; --profile overrides it"
else
  bad "C13-12" "profile selection by variable or flag did not work"
fi

run profunknown -- list-config --profile nosuchprofile
[ "$(rc profunknown)" = "3" ] && ok "C13-13" "an unknown profile is config" \
                              || bad "C13-13" "unknown profile gave exit $(rc profunknown), expected 3"

run proffile -- list-config --profile FILE
run profdefault -- list-config --profile default
case "$(rc proffile) $(rc profdefault)" in
  [23]\ [23]) ok "C13-16" "profiles named FILE and default are rejected" ;;
  *)   bad "C13-16" "--profile FILE / default gave exits $(rc proffile) / $(rc profdefault), expected usage or config" ;;
esac

# §7.3 list-profiles: [] with nothing declared; otherwise default first, then names; names only.
if [ "$(rc lp)" = "0" ] && [ "$(jq -c '.data' "$WORK/out/lp.json" 2>/dev/null)" = "[]" ]; then
  ok "C7-9" "list-profiles is [] when no profile is declared"
else
  bad "C7-9" "list-profiles with nothing declared gave exit $(rc lp): $(head -c 200 "$WORK/out/lp.json")"
fi
printf '{"default":{"limit":7},"profiles":{"probe":{"limit":8}}}' >"$WORK/cfg/lp.json"
run lpmixed "${P}_TIMEOUT_PROBE=5s" "${P}_LIMIT_ENVONLY=6" -- list-profiles --config "$WORK/cfg/lp.json"
if jq -e '.data | map(.name) == ["default","envonly","probe"]
          and .[0].active == true and .[0].declared_in == [] and ([.[1:][] | .active] | any | not)
          and (.[1].declared_in == ["environment"])
          and (.[2].declared_in | sort == ["config_file","environment"])
          and all(.[]; (keys - ["description"]) == ["active","declared_in","name"])' "$WORK/out/lpmixed.json" >/dev/null 2>&1; then
  ok "C7-10" "list-profiles lists default first, then file and environment profiles by name, names (and descriptions) only"
else
  bad "C7-10" "list-profiles output wrong: $(jq -c '.data' "$WORK/out/lpmixed.json" 2>/dev/null | head -c 300)"
fi
run lpactive "${P}_TIMEOUT_PROBE=5s" -- list-profiles --config "$WORK/cfg/lp.json" --profile probe
if jq -e '[.data[] | select(.active) | .name] == ["probe"]' "$WORK/out/lpactive.json" >/dev/null 2>&1; then
  ok "C7-11" "list-profiles marks the selected profile active"
else
  bad "C7-11" "the selected profile was not the only active entry"
fi
run lpunknown -- list-profiles --config "$WORK/cfg/lp.json" --profile nosuchprofile
if [ "$(rc lpunknown)" = "0" ] && jq -e '([.data[] | select(.active)] | length == 0) and (.meta.warnings | length > 0)' \
     "$WORK/out/lpunknown.json" >/dev/null 2>&1; then
  ok "C7-12" "an undeclared selected profile gives a warning, not a failure"
else
  bad "C7-12" "list-profiles --profile nosuchprofile gave exit $(rc lpunknown) or no warning"
fi
if jq -e '.error.hint | test("list-profiles")' "$WORK/out/profunknown.json" >/dev/null 2>&1; then
  ok "C13-18" "the undeclared-profile hint points to list-profiles"
else
  warn "C13-18" "the undeclared-profile hint does not mention list-profiles (a SHOULD)"
fi

# Optional profile descriptions (contract 1.4.1, §13.4 rule 6, §7.3). A tool that rejects the member as
# unrecognised does not support them, and these checks are skipped.
printf '{"default":{"description":"Default probe."},"profiles":{"probe":{"description":"Probe profile."},"plain":{"limit":5}}}' \
  >"$WORK/cfg/desc.json"
run descsupport "${P}_LIMIT_ENVONLY=6" -- list-profiles --config "$WORK/cfg/desc.json"
# The skip applies only to the unrecognised-member rejection of description (exit 3, config, and the
# message or hint names description and says it is unrecognised). Any other failure is a FAIL, so a supporting tool with a broken
# validator cannot skip silently.
if [ "$(rc descsupport)" = "3" ] &&
   jq -e '.error.code == "config" and (((.error.message // "") + " " + (.error.hint // "")) | test("description") and test("unrecogni[sz]ed|unknown|unsupported|not recogni[sz]ed"; "i"))' \
     "$WORK/out/descsupport.json" >/dev/null 2>&1; then
  skip "C7-13" "descriptions not supported by this tool"
  skip "C7-14" "descriptions not supported by this tool"
  skip "C13-19" "descriptions not supported by this tool"
elif [ "$(rc descsupport)" != "0" ]; then
  bad "C7-13" "list-profiles on a config with valid descriptions gave exit $(rc descsupport): $(jq -r '.error.message // empty' "$WORK/out/descsupport.json" 2>/dev/null | head -c 200)"
  bad "C7-14" "not checked: list-profiles failed on a config with valid descriptions"
  bad "C13-19" "not checked: list-profiles failed on a config with valid descriptions"
else
  if jq -e '.data | map(.name) == ["default","envonly","plain","probe"]
            and all(.[]; has("description"))
            and ((.[] | select(.name == "default") | .description) == "Default probe.")
            and ((.[] | select(.name == "probe") | .description) == "Probe profile.")
            and ((.[] | select(.name == "plain") | .description) == null)
            and ((.[] | select(.name == "envonly") | .description) == null)' "$WORK/out/descsupport.json" >/dev/null 2>&1; then
    ok "C7-13" "list-profiles carries description on every entry, null when unset or environment-only"
  else
    bad "C7-13" "list-profiles descriptions wrong: $(jq -c '.data' "$WORK/out/descsupport.json" 2>/dev/null | head -c 400)"
  fi
  run desclc -- list-config --config "$WORK/cfg/desc.json" --profile probe
  if [ "$(rc desclc)" = "0" ] &&
     jq -e '[.data.settings[].name | ascii_downcase] | index("description") | not' "$WORK/out/desclc.json" >/dev/null 2>&1; then
    ok "C7-14" "description is not a setting: absent from list-config"
  else
    bad "C7-14" "list-config exit $(rc desclc), or it lists description as a setting"
  fi
  long=$(printf 'x%.0s' $(seq 201))
  descbad=""
  i=0
  for variant in \
      "{\"profiles\":{\"probe\":{\"description\":\"$long\"}}}" \
      "{\"default\":{\"description\":\"$long\"}}" \
      '{"profiles":{"probe":{"description":"line one\nline two"}}}' \
      '{"default":{"description":"line one\rline two"}}' \
      '{"profiles":{"probe":{"description":7}}}' \
      '{"profiles":{"probe":{"description":"   "}}}' \
      '{"default":{"description":""}}'; do
    i=$((i+1))
    printf '%s' "$variant" >"$WORK/cfg/descbad$i.json"
    run descbad$i -- list-profiles --config "$WORK/cfg/descbad$i.json"
    if [ "$(rc descbad$i)" != "3" ] || ! jq -e '.error.code == "config" and ((.error.hint // "") | test("description"))' \
         "$WORK/out/descbad$i.json" >/dev/null 2>&1; then
      descbad="$descbad $i"
    fi
  done
  [ -z "$descbad" ] && ok "C13-19" "a description over 200 characters, multi-line, non-string, or blank is config with a pointer hint" \
                    || bad "C13-19" "invalid descriptions not rejected as config with a hint naming description (variants:$descbad)"
fi

# Optional profile-scoped credentials (contract 1.4.2, §12.1). Run only when describe marks a credential
# profile_scoped:true; otherwise skipped. Credentials that are not profile-scoped get a dummy value in the
# default scope in every case, so only the profile-scoped one decides each outcome.
PSC=$(jq -r '[.data.auth.credentials[]? | select(.profile_scoped == true)][0].name // empty' "$WORK/out/describe.json")
PSC_IDS="C12-8 C12-9 C12-10 C12-11 C7-15 C7-16"
if [ -z "$PSC" ]; then
  for id in $PSC_IDS; do skip "$id" "no credential is marked profile_scoped"; done
else
  PSC_ENV=$(jq -r --arg n "$PSC" '.data.auth.credentials[] | select(.name == $n) | .env' "$WORK/out/describe.json")
  PSC_LC=$(printf '%s' "$PSC" | tr '[:upper:]' '[:lower:]')
  PSC_VALUE="scopedvalue${RANDOM}${RANDOM}ZZ"
  OTHER_VARS=()
  for v in $(jq -r --arg n "$PSC" '.data.auth.credentials[] | select(.name != $n) | .env' "$WORK/out/describe.json"); do
    OTHER_VARS+=("$v=dummy-not-a-real-key")
  done
  printf '%s\n' "$PSC_VALUE" >"$WORK/cfg/psc-own.key"; chmod 600 "$WORK/cfg/psc-own.key"
  printf '{"profiles":{"probe":{"%s_file":"%s"}}}' "$PSC_LC" "$WORK/cfg/psc-own.key" >"$WORK/cfg/psc-own.json"
  printf '{"profiles":{"probe":{"limit":8}}}' >"$WORK/cfg/psc-none.json"
  psc_row() { jq -c --arg n "$PSC" '.data.settings[] | select(.name == $n)' "$WORK/out/$1.json" 2>/dev/null; }

  # C12-8: the profile's own file wins over an exported default-scope variable (and a default-scope
  # file variable naming a file that does not exist, which must not even be opened).
  run pscown "$PSC_ENV=$PSC_VALUE" "${PSC_ENV}_FILE=$WORK/cfg/absent.key" ${OTHER_VARS[@]+"${OTHER_VARS[@]}"} \
      -- list-config --config "$WORK/cfg/psc-own.json" --profile probe
  if [ "$(rc pscown)" = "0" ] &&
     jq -e '.set == true and .origin == "file_profile" and .value == null' <<<"$(psc_row pscown)" >/dev/null 2>&1; then
    ok "C12-8" "a profile's own credential file beats an exported default-scope variable (origin file_profile)"
  else
    bad "C12-8" "profile-scoped credential did not come from the profile's own file: $(psc_row pscown | head -c 300) (exit $(rc pscown))"
  fi

  # C12-9, C7-15, C7-16: only the default scope has it.
  run pscdef "$PSC_ENV=$PSC_VALUE" ${OTHER_VARS[@]+"${OTHER_VARS[@]}"} -- doctor --config "$WORK/cfg/psc-none.json" --profile probe
  if [ "$(rc pscdef)" = "3" ] &&
     jq -e --arg env "$PSC_ENV" '.error.code == "config" and .error.details.profile == "probe"
           and .error.details.would_use_source == $env and ((.error.hint // "") | length > 0)' "$WORK/out/pscdef.json" >/dev/null 2>&1 &&
     ! grep -q "$PSC_VALUE" "$WORK/out/pscdef.json" "$WORK/out/pscdef.err"; then
    ok "C12-9" "default scope only: config, exit 3, details.profile and would_use_source (a name, never a value)"
  else
    bad "C12-9" "profile with only a default-scope credential did not fail as config/3 with would_use_source (exit $(rc pscdef))"
  fi
  if jq -e '(.data.checks | map(select(.name == "credentials")) | .[0].status == "fail")
            and (.data.checks | map(select(.name == "auth")) | .[0].status == "skip")' "$WORK/out/pscdef.json" >/dev/null 2>&1; then
    ok "C7-16" "doctor fails credentials for a profile with no credential of its own and skips auth"
  else
    bad "C7-16" "doctor did not fail credentials and skip auth for a profile with only a default-scope credential"
  fi
  run pscdeflc "$PSC_ENV=$PSC_VALUE" ${OTHER_VARS[@]+"${OTHER_VARS[@]}"} -- list-config --config "$WORK/cfg/psc-none.json" --profile probe
  if [ "$(rc pscdeflc)" = "0" ] &&
     jq -e --arg env "$PSC_ENV" '.set == false and .value == null and .source == "builtin" and .origin == "builtin"
           and ((.hint // "") | contains($env))' <<<"$(psc_row pscdeflc)" >/dev/null 2>&1; then
    ok "C7-15" "list-config reports set:false with a hint naming the skipped default-scope source"
  else
    bad "C7-15" "list-config row for a skipped default-scope credential wrong: $(psc_row pscdeflc | head -c 300)"
  fi

  # C12-10: neither scope has it.
  run pscnone ${OTHER_VARS[@]+"${OTHER_VARS[@]}"} -- doctor --config "$WORK/cfg/psc-none.json" --profile probe
  if [ "$(rc pscnone)" = "4" ] && jq -e '.error.code == "auth"' "$WORK/out/pscnone.json" >/dev/null 2>&1; then
    ok "C12-10" "no credential in either scope is auth, exit 4"
  else
    bad "C12-10" "profile with no credential anywhere gave exit $(rc pscnone), expected auth/4"
  fi

  # C12-11: with no profile, the default scope applies unchanged.
  run pscnoprof "$PSC_ENV=$PSC_VALUE" ${OTHER_VARS[@]+"${OTHER_VARS[@]}"} -- list-config
  if jq -e '.set == true and .origin == "env_default" and .source != null' <<<"$(psc_row pscnoprof)" >/dev/null 2>&1; then
    ok "C12-11" "without a profile the default-scope variable is used (env_default)"
  else
    bad "C12-11" "without a profile the default-scope credential was not used: $(psc_row pscnoprof | head -c 300)"
  fi
fi

run barenum "${P}_TIMEOUT=30" -- list-config
[ "$(rc barenum)" = "3" ] && ok "C13-17" "a duration without a unit is config" \
                          || bad "C13-17" "${P}_TIMEOUT=30 gave exit $(rc barenum), expected 3"

# ---------------------------------------------------------------------------- §14, §15, §16, §17, §18

section "§14 Determinism, §15 --human, §16 MCP, §17 reserved names, §18 versioning"

run describe2 -- describe
cmp -s "$WORK/out/describe.json" "$WORK/out/describe2.json" && ok "C14-1" "identical calls give identical bytes" \
                                                          || bad "C14-1" "two describe calls differed"

run pretty -- tools --pretty
[ "$(jq -S -c . "$WORK/out/pretty.json" 2>/dev/null)" = "$(jq -S -c . "$WORK/out/tools.json")" ] \
  && ok "C14-2" "--pretty changes only whitespace" || bad "C14-2" "--pretty changed the document"

run human -- tools --human
if [ "$(rc human)" = "0" ] && [ -s "$WORK/out/human.json" ] && ! jq -e . "$WORK/out/human.json" >/dev/null 2>&1; then
  ok "C15-1" "--human renders non-JSON output, exit 0"
else
  bad "C15-1" "--human produced JSON, nothing, or a non-zero exit"
fi

run humanerr -- no-such-command --human
if [ "$(rc humanerr)" = "2" ] && grep -q "usage" "$WORK/out/humanerr.json"; then
  ok "C15-2" "--human keeps the exit code and shows the error code"
else
  bad "C15-2" "--human on an error exited $(rc humanerr) or hid the error code"
fi

run humanpretty -- tools --human --pretty
[ "$(rc humanpretty)" = "2" ] && ok "C15-3" "--human with --pretty is usage" \
                              || bad "C15-3" "--human --pretty gave exit $(rc humanpretty), expected 2"

MCP=$(jq -r '.data.mcp_enabled' "$WORK/out/describe.json")
case "$MCP" in
  false)
    run serve -- serve
    if [ "$(rc serve)" = "8" ] && jq -e '.error.details.reason == "mcp_disabled"' "$WORK/out/serve.json" >/dev/null 2>&1; then
      ok "C16-1" "serve without MCP built is refused with mcp_disabled"
    else
      bad "C16-1" "serve on a build without MCP gave exit $(rc serve), not refused/mcp_disabled"
    fi ;;
  true) skip "C16-1" "MCP is built in; test the server with an MCP client" ;;
  *)    bad "C16-1" "describe.mcp_enabled is not a boolean" ;;
esac

# §17 reserved flags and §12.2 credential flags may never be a command's own parameter flag.
RESERVED_FLAGS='["--pretty","--human","--fields","--keep-empty","--limit","--cursor","--max-bytes","--max-string",
  "--max-depth","--max-pages","--since","--until","--all","--offset","--confirm","--dry-run","--profile","--config",
  "--dataset-dir","--timeout","--budget","--timing","--deterministic","--verbose","--schema","--detail","--list",
  "--transport","--addr","--allow-remote","--token-file","--tls-cert-file","--tls-key-file",
  "--tls-terminated-upstream","--version","--help","--api-key","--key","--token","--password","--secret","--credential"]'
clashes=$(jq -c --argjson reserved "$RESERVED_FLAGS" \
  '[.data.commands[] | .name as $n | (."x-cli".flags // {}) | to_entries[]
    | select(.value as $f | $reserved | index($f)) | "\($n) \(.value)"]' "$WORK/out/describe.json")
[ "$clashes" = "[]" ] && ok "C17-1" "no parameter flag is a reserved or credential flag" \
                      || bad "C17-1" "parameter flags reuse reserved names: $clashes"

run version -- version
run version2 -- --version
if jq -e '.data | has("tool") and has("tool_version") and has("contract_version") and has("writes_enabled")
          and has("mcp_enabled") and has("os") and has("arch")' "$WORK/out/version.json" >/dev/null 2>&1 &&
   [ "$(jq -c '.data' "$WORK/out/version.json")" = "$(jq -c '.data' "$WORK/out/version2.json")" ]; then
  ok "C18-1" "version and --version agree and carry the required fields"
else
  bad "C18-1" "version output incomplete, or version and --version differ"
fi

# ---------------------------------------------------------------------------- run-wide checks

section "Across every invocation"

if grep -l $'\x1b' "$WORK"/out/* >/dev/null 2>&1; then
  bad "C2-3" "ANSI escapes in: $(grep -l $'\x1b' "$WORK"/out/* | xargs -n1 basename | tr '\n' ' ')"
else
  ok "C2-3" "no ANSI escape sequences on any stream"
fi

# C4-1: every error envelope produced pairs code and exit exactly as the contract's table does.
TABLE='{"internal":1,"usage":2,"config":3,"auth":4,"not_found":5,"validation":6,"conflict":7,"refused":8,"rate_limited":9,"timeout":10,"network":11,"upstream":12,"partial":13,"cache_miss":14,"canceled":130}'
mismatch=""
for f in "$WORK"/out/*.json; do
  n=$(basename "$f" .json); [ -f "$WORK/out/$n.rc" ] || continue
  code=$(jq -r 'select(.ok == false) | .error.code' "$f" 2>/dev/null) || continue
  [ -z "$code" ] && continue
  want=$(jq -r --arg c "$code" '.[$c] // empty' <<<"$TABLE")
  got=$(rc "$n")
  [ -z "$want" ] && want=$(jq -r --arg c "$code" '.data.exit_codes[]? | select(.name == $c) | .code' "$WORK/out/describe.json")
  if [ "$code" = "canceled" ]; then [ "$got" = "130" ] || [ "$got" = "143" ] || mismatch="$mismatch $n:$code/$got"
  elif [ "$want" != "$got" ]; then mismatch="$mismatch $n:$code/$got"; fi
done
[ -z "$mismatch" ] && ok "C4-1" "every error paired its code and exit status correctly" \
                   || bad "C4-1" "code/exit mismatches:$mismatch"

if jq -e '[.data.exit_codes[]? | .code >= 32 and .code <= 63] | all' "$WORK/out/describe.json" >/dev/null 2>&1; then
  ok "C4-2" "tool-specific exit codes are within 32–63"
else
  bad "C4-2" "a tool-specific exit code is outside 32–63"
fi

printf '\npassed %d   failed %d   warnings %d   skipped %d\n' "$PASS" "$FAIL" "$WARN" "$SKIP"
echo "Items marked manual in references/conformance-checklist.md are not covered by this script."
[ "$FAIL" -eq 0 ]

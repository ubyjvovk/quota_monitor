#!/usr/bin/env bash
# Isolated test runner: full output goes to a log file, stdout gets a bounded
# summary. This is the only way anyone (worker or PM) runs tests — it keeps
# raw test output out of LLM context while preserving the full log for
# drill-down via grep.
set -u
cd "$(dirname "$0")/../.." || exit 1
TIGERTEAM_DIR="${TIGERTEAM_DIR:-.tigerteam}"

# Resolution order (Config v3 — the runner already injects TIGERTEAM_TEST_CMD
# into every engine env, so this is only for a manual wrapper run):
#   1. $TIGERTEAM_TEST_CMD when set and non-empty
#   2. `tigerteam config get test_cmd` when the CLI is on PATH (stderr discarded)
#   3. hard error naming the env var and tigerteam.toml
if [ -z "${TIGERTEAM_TEST_CMD:-}" ] && command -v tigerteam >/dev/null 2>&1; then
  TIGERTEAM_TEST_CMD="$(tigerteam config get test_cmd 2>/dev/null)"
fi

if [ -z "${TIGERTEAM_TEST_CMD:-}" ]; then
  echo "run-tests: TIGERTEAM_TEST_CMD not set (no test_cmd in tigerteam.toml either)" >&2
  exit 2
fi

mkdir -p "$TIGERTEAM_DIR/logs/tests"
ts="$(date -u +%Y%m%d-%H%M%S)"
logfile="$TIGERTEAM_DIR/logs/tests/${ts}-$$.log"

bash -c "$TIGERTEAM_TEST_CMD \"\$@\"" runner "$@" > "$logfile" 2>&1
rc=$?

lines=$(wc -l < "$logfile" | tr -d ' ')
echo "cmd:  $TIGERTEAM_TEST_CMD $*"
echo "exit: $rc"
echo "log:  $logfile ($lines lines — grep it for specific failures; do not read it whole)"
if [ "$lines" -le 40 ]; then
  echo "--- full output ---"
  cat "$logfile"
else
  echo "--- result lines ---"
  grep -Ei '([0-9]+ (passed|failed|errors?|skipped|warnings?)|^(FAILED|ERROR|PASS|FAIL|ok )|✓|✗|Tests:|Suites:)' "$logfile" | tail -n 20
  echo "--- last 25 lines ---"
  tail -n 25 "$logfile"
fi
exit "$rc"

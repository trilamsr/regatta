#!/usr/bin/env bash
# check-no-bare-sleep_test.sh - fixture cases for check-no-bare-sleep.sh.
#
# The gate forbids `time.Sleep` lexically nested inside a `for` block in any
# `*_test.go` file outside the `_bench_test.go` allowlist. The migration
# target is testutil.Eventually / testutil.EventuallyT / testutil.AssertStable
# — state-driven settle that fails fast and short-circuits on success.
#
# Cases:
#   A. clean test (no Sleep)                  -> exit 0
#   B. Sleep inside `for {}` (polling)        -> exit 1, error names file+line
#   C. Sleep + `// allow-sleep:` directive    -> exit 0
#   D. `_bench_test.go` filename suffix       -> exit 0 (benchmarks measure wall-clock)
#   E. Standalone Sleep (for is unrelated)    -> exit 0
#   F. `for range`-style polling Sleep        -> exit 1
#   G. Sleep in nested `for` inside test      -> exit 1

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-no-bare-sleep.sh"
FIXTURES="$REPO_ROOT/scripts/testdata/no-bare-sleep"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <name> <expected-exit> <fixture-file> [grep-pattern]
  local name="$1" want_exit="$2" fixture="$3" pattern="${4:-}"
  local tmp out got_exit
  tmp=$(mktemp -d)
  cp "$fixture" "$tmp/"
  out=$(TESTS_ROOT="$tmp" bash "$CHECK" 2>&1)
  got_exit=$?
  local ok=1
  if [ "$got_exit" -ne "$want_exit" ]; then
    ok=0
  elif [ -n "$pattern" ] && ! printf '%s\n' "$out" | grep -qE "$pattern"; then
    ok=0
  fi
  if [ "$ok" -eq 1 ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=$got_exit)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=$want_exit got=$got_exit pattern=${pattern:-<none>}"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
  rm -rf "$tmp"
}

run_case "A-clean"                       0 "$FIXTURES/clean_test.go"
run_case "B-polling-violation-fails"     1 "$FIXTURES/polling_violation_test.go" \
  "polling_violation_test\.go:.*time\.Sleep"
run_case "C-allowlisted-directive"       0 "$FIXTURES/allowlisted_test.go"
run_case "D-bench-suffix-allowed"        0 "$FIXTURES/sweeper_load_bench_test.go"
run_case "E-standalone-sleep-allowed"    0 "$FIXTURES/standalone_sleep_test.go"
run_case "F-for-range-poll-fails"        1 "$FIXTURES/for_range_chan_poll_test.go" \
  "for_range_chan_poll_test\.go:.*time\.Sleep"
run_case "G-nested-for-poll-fails"       1 "$FIXTURES/nested_for_poll_test.go" \
  "nested_for_poll_test\.go:.*time\.Sleep"

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0

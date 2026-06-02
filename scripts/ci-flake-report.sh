#!/usr/bin/env bash
# ci-flake-report: rank tests by flake rate across recent CI runs.
#
# Scans the last $RUN_LIMIT GitHub Actions runs (default 100), pulls the
# failed-step logs for each failed run, extracts Go `--- FAIL: TestXxx`
# lines, and tallies per-test fail counts. Flake rate is computed as
# fails / total_scanned_runs - an honest lower-bound signal since
# successful-run logs are not fetched (would 10x the network cost).
#
# Output: a table of the top N tests ordered by fail count (highest
# first), with fail count, scanned-run count, and observed flake rate.
#
# Why: repeatedly-failing tests block merge waves. Surfacing them
# programmatically beats grepping individual run logs by hand.
#
# Usage:
#   bash scripts/ci-flake-report.sh                  # default: top 10, last 100 runs
#   TOP=20 bash scripts/ci-flake-report.sh           # top 20
#   RUN_LIMIT=50 bash scripts/ci-flake-report.sh     # narrower window
#   WORKFLOW=CI bash scripts/ci-flake-report.sh      # restrict to one workflow
#   bash scripts/ci-flake-report.sh --help           # show this header

set -euo pipefail

# Argument parsing.
for arg in "$@"; do
  case "$arg" in
    -h|--help)
      sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *)
      echo "ci-flake-report: unknown flag: $arg" >&2
      exit 2 ;;
  esac
done

# Tunables. Override via environment.
RUN_LIMIT="${RUN_LIMIT:-100}"
TOP="${TOP:-10}"
WORKFLOW="${WORKFLOW:-}"

# Dependency probe; fail loudly so the operator does not chase a silent
# empty-table later.
for bin in gh jq awk sort; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "ci-flake-report: missing dependency: $bin" >&2
    exit 3
  fi
done

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

# Step 1 - enumerate recent runs as a JSON array. Note: we ask for both
# success and failure conclusions because the flake denominator is
# (fails + passes), not just fails.
runs_json=$(gh run list --limit "$RUN_LIMIT" \
  --json databaseId,conclusion,workflowName,createdAt,headBranch \
  2>/dev/null || echo "[]")

# Optional workflow filter.
if [ -n "$WORKFLOW" ]; then
  runs_json=$(echo "$runs_json" | jq --arg w "$WORKFLOW" '[.[] | select(.workflowName == $w)]')
fi

total_runs=$(echo "$runs_json" | jq 'length')
fail_runs=$(echo "$runs_json" | jq '[.[] | select(.conclusion == "failure")] | length')
ok_runs=$(echo "$runs_json"   | jq '[.[] | select(.conclusion == "success")] | length')

printf 'ci-flake-report: scanned %s runs (%s failed, %s succeeded%s)\n' \
  "$total_runs" "$fail_runs" "$ok_runs" \
  "${WORKFLOW:+, workflow=$WORKFLOW}"

if [ "$fail_runs" -eq 0 ]; then
  echo "ci-flake-report: no failed runs in window; nothing to tally."
  exit 0
fi

# Step 2 - per failed run, fetch the failed-step logs and grep Go test
# failures. The standard Go test runner emits `--- FAIL: TestName (Xs)`
# at the top of each failed test block; that line uniquely identifies
# the test even across packages.
#
# We accumulate `TestName` occurrences into a temp file so the final
# tally is a single sort|uniq pass. Using a tempfile (not an in-shell
# string) keeps memory bounded for large log dumps.
tmp_fails=$(mktemp -t ci-flake-fails.XXXXXX)
trap 'rm -f "$tmp_fails"' EXIT

failed_ids=$(echo "$runs_json" | jq -r '.[] | select(.conclusion == "failure") | .databaseId')

# Progress feedback - each run takes ~1s of network. Print a dot per
# fetched run so the operator knows the script is alive.
printf 'ci-flake-report: fetching failed logs '
n=0
while IFS= read -r rid; do
  [ -z "$rid" ] && continue
  n=$((n + 1))
  # `gh run view --log-failed` returns just the failed steps; redirect
  # stderr so a transient API error does not abort the whole report.
  gh run view --log-failed "$rid" 2>/dev/null \
    | grep -oE -- '--- FAIL: [A-Za-z_][A-Za-z0-9_/]*' \
    | awk '{print $3}' \
    | sort -u \
    >> "$tmp_fails" || true
  printf '.'
done <<< "$failed_ids"
printf ' done (%s runs)\n' "$n"

# Step 3 - tally. `sort | uniq -c` gives "  N TestName"; awk reshapes
# into "TestName\tN" so downstream formatting can sort numerically.
if [ ! -s "$tmp_fails" ]; then
  echo "ci-flake-report: no Go test failures matched in the failed logs."
  echo "  (non-test failures - lint, build, infra - are not counted)"
  exit 0
fi

tally=$(sort "$tmp_fails" | uniq -c | awk '{name=$2; for(i=3;i<=NF;i++) name=name" "$i; printf "%s\t%s\n", name, $1}')

# Step 4 - render the top N. Flake rate denominator is the number of
# scanned runs - a test that fails in 5/100 runs has a 5% observed
# flake rate against the recent window. We do not attempt to attribute
# success runs to individual tests because the green-path logs are not
# fetched (cost: 100 runs * ~1MB each); fail-count / total-runs is the
# honest signal here.
printf '\n%-60s  %6s  %6s  %s\n' "TEST" "FAILS" "RUNS" "FLAKE_RATE"
printf '%-60s  %6s  %6s  %s\n' \
  "------------------------------------------------------------" \
  "------" "------" "----------"

echo "$tally" \
  | sort -t$'\t' -k2,2nr \
  | head -n "$TOP" \
  | awk -F'\t' -v total="$total_runs" '
      {
        fails = $2
        rate  = (total > 0) ? (fails * 100.0 / total) : 0
        printf "%-60s  %6d  %6d  %6.2f%%\n", $1, fails, total, rate
      }
    '

echo
echo "ci-flake-report: window=${RUN_LIMIT} runs; showing top ${TOP} by fail count."
echo "  Note: flake_rate = fails / total_scanned_runs. Tests that did"
echo "  not fail at all are not listed."

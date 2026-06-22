#!/usr/bin/env bash
# check-no-bare-time-unix.sh - fail when `time.Unix(` appears in any *.go
# file without a chained `.UTC()` call OR `// allow-bare-time-unix:`
# directive. Closes the R22 class: state/runs.go scanned int64 unix
# timestamps into `time.Unix(sec, 0)` (defaults to Local zone); the
# dashboard formatter assumes UTC, so on a non-UTC host the rendered
# StartedAt/FinishedAt drifted by the local offset.
#
# Rule: every `time.Unix(` call must `.UTC()` at the same statement —
# the storage boundary normalizes once; downstream code can assume UTC.
#
# Allowed:
#   time.Unix(sec, 0).UTC()
#   t := time.Unix(sec, 0); t = t.UTC()  // multi-step OK if both within 3 lines
#   time.Unix(sec, 0) // allow-bare-time-unix: deliberately Local for X
#
# Scope: every tracked *.go under REPO_ROOT (defaults to repo root).
# Excludes *_test.go (tests may use Local for clock-skew assertions).
# Override via REPO_ROOT for the fixture test runner.
#
# Exit: 0 clean, 1 on first violation (lists every hit before exit).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
: "${REPO_ROOT:=$(cd -- "$SCRIPT_DIR/.." && pwd)}"

if [ ! -d "$REPO_ROOT" ]; then
  echo "check-no-bare-time-unix: REPO_ROOT not found: $REPO_ROOT (skipping)"
  exit 0
fi

cd "$REPO_ROOT"

violations=0
while IFS= read -r line; do
  file=${line%%:*}
  rest=${line#*:}
  lineno=${rest%%:*}
  content=${rest#*:}
  case "$file" in *_test.go) continue;; esac
  # `.UTC()` chained on same line — OK.
  if echo "$content" | grep -Eq 'time\.Unix\([^)]*\)\.UTC\('; then
    continue
  fi
  # Directive escape on same line — OK.
  if echo "$content" | grep -Eq '// allow-bare-time-unix:'; then
    continue
  fi
  echo "check-no-bare-time-unix: $line"
  violations=$((violations + 1))
done < <(git grep -nE 'time\.Unix\(' -- '*.go' 2>/dev/null || true)

if [ "$violations" -gt 0 ]; then
  echo "check-no-bare-time-unix: $violations violation(s). Append .UTC() at the call site or annotate '// allow-bare-time-unix: <reason>'."
  exit 1
fi

echo "check-no-bare-time-unix: ok"
exit 0

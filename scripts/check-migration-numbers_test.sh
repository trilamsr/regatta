#!/usr/bin/env bash
# check-migration-numbers_test.sh — fixture cases for check-migration-numbers.sh.
#
# The gate fails closed when internal/orchestrator/state/migrations/ carries
# duplicate version numbers, a non-contiguous tail, or when a PR diff adds
# more than one new NNNN_*.sql file without a justification comment.
#
# Cases:
#   A. clean sequential dir (0001..0005)            -> exit 0
#   B. duplicate version (0001, 0002, 0002_dup)     -> exit 1, "duplicate version 2"
#   C. non-contiguous tail (0001, 0002, 0004)       -> exit 1, "non-contiguous tail"
#   D. known-gap passes when KNOWN_GAPS=(3)         -> exit 0
#   E. multi-add PR (two added migrations, no just) -> exit 1, "adds 2 migrations"
#   F. justified multi-add (PR body carries marker) -> exit 0
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-migration-numbers.sh"
FIXTURES="$REPO_ROOT/scripts/testdata/check-migration-numbers"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <name> <expected-exit> <env-prefix> [grep-pattern]
  local name="$1" want_exit="$2" envs="$3" pattern="${4:-}"
  local out got_exit
  out=$(env -i HOME="$HOME" PATH="$PATH" bash -c "$envs bash '$CHECK'" 2>&1)
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
}

# DIFF_ADDED is a test seam: when set, the script uses its newline-separated
# content instead of running `git diff`. PR_BODY is a test seam: when set, the
# script reads it instead of calling `gh pr view`.

run_case "A-clean-passes" 0 \
  "MIGRATIONS_DIR='$FIXTURES/clean' DIFF_ADDED='' "

run_case "B-duplicate-fails" 1 \
  "MIGRATIONS_DIR='$FIXTURES/duplicate' DIFF_ADDED='' " \
  "duplicate version 2"

run_case "C-non-contiguous-tail-fails" 1 \
  "MIGRATIONS_DIR='$FIXTURES/non-contiguous-tail' DIFF_ADDED='' " \
  "non-contiguous tail"

run_case "D-known-gap-passes" 0 \
  "MIGRATIONS_DIR='$FIXTURES/known-gap' KNOWN_GAPS='3' DIFF_ADDED='' "

run_case "E-multi-add-fails" 1 \
  "MIGRATIONS_DIR='$FIXTURES/clean' DIFF_ADDED=\$'0006_a.sql\n0007_b.sql' " \
  "adds 2 migrations"

run_case "F-multi-add-justified-passes" 0 \
  "MIGRATIONS_DIR='$FIXTURES/clean' DIFF_ADDED=\$'0006_a.sql\n0007_b.sql' PR_BODY='see <!-- migration-multi-add-justified: parent+child FK --> end' "

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0

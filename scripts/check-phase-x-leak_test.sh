#!/usr/bin/env bash
# check-phase-x-leak_test.sh - fixture cases for check-phase-x-leak.sh.
#
# The gate is mechanical enforcement of the self-host filter in
# docs/engineer/briefs/2026-06-01-self-host-first.md §1 + §4 (Phase X is
# deferred until an external customer ask). Active specs MUST NOT silently
# absorb Phase-X scope; non-active or `phase: x-forward-fit`-opted specs
# may name the tokens.
#
# Cases:
#   A. active + no Phase-X token            -> exit 0
#   B. active + Phase-X token (no opt-in)   -> exit 1, error names file+token
#   C. status: skeleton-prefetch            -> exit 0 (always allowed)
#   D. status: shipped                       -> exit 0 (skip)
#   E. status: archived                      -> exit 0 (skip)
#   F. missing status, Phase-X token         -> exit 1 (missing == active)
#   G. active + phase: x-forward-fit opt-in -> exit 0
#   H. active + tokens only inside ``` fence -> exit 0 (literal/code exempt)

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-phase-x-leak.sh"
FIXTURES="$REPO_ROOT/scripts/testdata/phase-x-leak"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <name> <expected-exit> <fixture-file> [grep-pattern]
  local name="$1" want_exit="$2" fixture="$3" pattern="${4:-}"
  local tmp out got_exit
  tmp=$(mktemp -d)
  cp "$fixture" "$tmp/"
  out=$(SPECS_DIR="$tmp" bash "$CHECK" 2>&1)
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

run_case "A-active-clean"                0 "$FIXTURES/spec-active-clean.md"
run_case "B-active-leak-fails"           1 "$FIXTURES/spec-active-leak.md" \
  "spec-active-leak\.md.*tenant_id"
run_case "C-skeleton-prefetch-allowed"   0 "$FIXTURES/spec-skeleton-prefetch.md"
run_case "D-shipped-skipped"             0 "$FIXTURES/spec-shipped-with-tokens.md"
run_case "E-archived-skipped"            0 "$FIXTURES/spec-archived-with-tokens.md"
run_case "F-missing-status-leak-fails"   1 "$FIXTURES/spec-missing-status-leak.md" \
  "spec-missing-status-leak\.md.*blackboard"
run_case "G-active-forward-fit-allowed"  0 "$FIXTURES/spec-active-forward-fit.md"
run_case "H-active-fenced-only-allowed"  0 "$FIXTURES/spec-active-fenced-mention.md"

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0

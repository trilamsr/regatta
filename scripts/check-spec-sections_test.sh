#!/usr/bin/env bash
# check-spec-sections_test.sh — fixture cases for check-spec-sections.sh.
#
# Cases:
#   A. complete fixture (STRICT=1)             -> exit 0
#   B. missing-acceptance (STRICT=1)           -> exit 1, "missing section(s): Acceptance"
#   C. missing-acceptance, NOT changed in PR   -> exit 0 (warn-only on pre-existing)
#   D. missing-acceptance, IS changed in PR    -> exit 1
#   E. skeleton-prefetch frontmatter           -> exit 0 (skipped entirely)
#   F. shipped status frontmatter              -> exit 0 (skipped entirely)
#   G. phase-x-deferred status frontmatter     -> exit 0 (skipped entirely)
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-spec-sections.sh"
FIXTURES="$REPO_ROOT/scripts/testdata/check-spec-sections"

passes=0
fails=0
failed_names=()

run_case() {
  local name="$1" want_exit="$2" envs="$3" pattern="${4:-}"
  local out got_exit
  out=$(bash -c "$envs bash '$CHECK'" 2>&1)
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

run_case "A-complete-passes" 0 \
  "SPECS_DIR='$FIXTURES/complete' STRICT=1 DIFF_FILES='' "

run_case "B-missing-acceptance-strict-fails" 1 \
  "SPECS_DIR='$FIXTURES/missing-acceptance' STRICT=1 DIFF_FILES='' " \
  "missing section\(s\): Acceptance"

# Pre-existing spec (not in PR diff) — should warn but NOT fail.
run_case "C-missing-acceptance-preexisting-passes" 0 \
  "SPECS_DIR='$FIXTURES/missing-acceptance' STRICT=0 DIFF_FILES='' "

# Spec IS in PR diff — should fail.
# The script's diff path is computed relative to REPO_ROOT, so DIFF_FILES
# must use the same prefix the script emits internally (rel path).
run_case "D-missing-acceptance-in-diff-fails" 1 \
  "SPECS_DIR='$FIXTURES/missing-acceptance' STRICT=0 DIFF_FILES='scripts/testdata/check-spec-sections/missing-acceptance/2026-06-08-fixture-missing-acceptance.md' " \
  "missing section\(s\): Acceptance"

run_case "E-skeleton-prefetch-passes" 0 \
  "SPECS_DIR='$FIXTURES/skeleton-prefetch' STRICT=1 DIFF_FILES='' "

run_case "F-shipped-status-passes" 0 \
  "SPECS_DIR='$FIXTURES/shipped' STRICT=1 DIFF_FILES='' "

run_case "G-phase-x-deferred-passes" 0 \
  "SPECS_DIR='$FIXTURES/phase-x-deferred' STRICT=1 DIFF_FILES='' "

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0

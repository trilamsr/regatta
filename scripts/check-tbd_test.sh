#!/usr/bin/env bash
# check-tbd_test.sh - fixture cases for check-tbd.sh.
#
# The gate fails closed when a doc file under docs/engineer/{specs,plans,briefs}
# carries a bare `TBD` placeholder (owner: TBD, issue umbrella: TBD, #TBD-followup,
# etc.). Bare `TBD` is unresolved prose debt; the resolution path is either
# (1) replace TBD with concrete content, (2) cite an issue `#NNN` on the same
# line, or (3) wrap the placeholder in an HTML comment `<!-- TODO(#NNN) -->`.
# A fourth allowance lets the operator type bare TBDs inside a ```release-notes
# fence while drafting a PR body in the spec.
#
# Cases:
#   A. clean doc (no TBD)                            -> exit 0
#   B. bare `TBD` outside fence + no issue ref       -> exit 1, names file
#   C. `TBD` with same-line `#NNN` citation          -> exit 0
#   D. `TBD` inside `<!-- TODO(#NNN) -->` comment    -> exit 0
#   E. bare `TBD` inside ```release-notes fence      -> exit 0 (operator scratch)
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-tbd.sh"
FIXTURES="$REPO_ROOT/scripts/testdata/check-tbd"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <name> <expected-exit> <fixture-file> [grep-pattern]
  local name="$1" want_exit="$2" fixture="$3" pattern="${4:-}"
  local tmp out got_exit
  tmp=$(mktemp -d)
  mkdir -p "$tmp/specs" "$tmp/plans" "$tmp/briefs"
  cp "$fixture" "$tmp/specs/"
  out=$(DOCS_ROOT="$tmp" bash "$CHECK" 2>&1)
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

run_case "A-clean-passes"                       0 "$FIXTURES/clean.md"
run_case "B-bare-tbd-fails"                     1 "$FIXTURES/bare-tbd.md" \
  "bare-tbd\.md.*TBD"
run_case "C-tbd-with-issue-passes"              0 "$FIXTURES/tbd-with-issue.md"
run_case "D-tbd-in-html-comment-passes"         0 "$FIXTURES/tbd-in-html-comment.md"
run_case "E-tbd-in-release-notes-fence-passes"  0 "$FIXTURES/tbd-in-release-notes-fence.md"

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0

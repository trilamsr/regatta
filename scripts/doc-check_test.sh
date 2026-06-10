#!/usr/bin/env bash
# doc-check_test.sh - assertions for scripts/doc-check.sh comment-noise gate.
#
# Each case builds a git repo w/ base commit, branches, feeds an added line
# to the diff, and asserts exit status. The gate exits 1 only when the
# added line shape looks like a review-cycle inline tag, not when prose
# mentions a "Reviewer <Capital>" sequence (#333).
#
# Banned-phrase case coverage removed alongside banned-phrase gate (#1266).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
DOC_CHECK="$REPO_ROOT/scripts/doc-check.sh"

passes=0
fails=0
failed_names=()

run_reviewer_case() {
  local name="$1" added="$2" want_exit="$3"
  local tmp got_exit out
  tmp=$(mktemp -d)
  out=$(
    cd "$tmp" || exit 99
    git init -q -b main . 2>/dev/null
    git config user.email t@t.t
    git config user.name t
    : > fixture.go
    git add fixture.go
    git commit -q -m base 2>/dev/null
    git checkout -q -b work
    printf '// %s\n' "$added" >> fixture.go
    git add fixture.go
    git commit -q -m add 2>/dev/null
    GITHUB_BASE_REF=main bash "$DOC_CHECK" 2>&1
  )
  got_exit=$?
  rm -rf "$tmp"

  if [ "$got_exit" -eq "$want_exit" ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=$got_exit)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=$want_exit got=$got_exit"
    echo "$out" | sed 's/^/     | /'
  fi
}

run_reviewer_case "reviewer-tag colon form matches"        "Reviewer Bob: nit"            1
run_reviewer_case "reviewer-tag hyphen+slash form matches" "Reviewer-Alice/round-2"       1
run_reviewer_case "prose Reviewer Request is exempt"       "Reviewer Request returns nil" 0
run_reviewer_case "compound -reviewer prose is exempt"     "Zero-reviewer Request"        0
run_reviewer_case "lowercase reviewer JSON prose exempt"   "reviewer JSON object"         0

echo
if [ "$fails" -gt 0 ]; then
  echo "doc-check_test: $fails failed, $passes passed"
  for n in "${failed_names[@]}"; do echo "  - $n"; done
  exit 1
fi
echo "doc-check_test: all $passes case(s) passed"

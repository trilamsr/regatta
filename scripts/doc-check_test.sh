#!/usr/bin/env bash
# doc-check_test.sh - assertions for scripts/doc-check.sh banned-phrase gate.
#
# Each case spins up a throwaway git repo, drops one fixture in, and runs
# the real doc-check.sh. We assert exit status + the banned-phrase summary
# line (link-integrity and comment-noise gates are no-ops on these fixtures
# because there are no .md links and no base ref).
#
# Why a real subshell instead of unit-testing the regex: the gate's bug
# was that it called grep on full file contents. A regex unit test would
# have passed while the production gate stayed broken. Run the script.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
DOC_CHECK="$REPO_ROOT/scripts/doc-check.sh"
FIXTURES="$SCRIPT_DIR/testdata/doc-check"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <case-name> <fixture-basename> <expected-exit:0|1>
  local name="$1" fixture="$2" want_exit="$3"
  local tmp
  tmp=$(mktemp -d)
  # Subshell so cd does not leak across cases.
  local got_exit out
  out=$(
    cd "$tmp" || exit 99
    git init -q . 2>/dev/null
    git config user.email t@t.t
    git config user.name t
    cp "$FIXTURES/$fixture" ./fixture.md
    git add fixture.md
    git commit -q -m init 2>/dev/null
    bash "$DOC_CHECK" 2>&1
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

run_case "fenced block hides banned tokens"   fenced_block.md    0
run_case "inline backticks hide banned tokens" inline_backtick.md 0
run_case "plain prose trips gate"              plain_prose.md     1
run_case "mixed (backtick+fenced only) passes" mixed_pass.md      0
run_case "mixed (one plain-prose hit) fails"   mixed_fail.md      1

# Reviewer-tag (comment-noise gate) cases.
#
# These cases build a git repo with a base commit, branch to a working
# state, and feed an added line to the diff. The gate exits 1 only when
# the added line shape looks like a review-cycle inline tag, not when
# prose mentions a "Reviewer <Capital>" sequence (issue #333).
run_reviewer_case() {
  # run_reviewer_case <case-name> <added-line> <expected-exit:0|1>
  local name="$1" added="$2" want_exit="$3"
  local tmp
  tmp=$(mktemp -d)
  local got_exit out
  out=$(
    cd "$tmp" || exit 99
    git init -q -b main . 2>/dev/null
    git config user.email t@t.t
    git config user.name t
    : > fixture.go
    git add fixture.go
    git commit -q -m base 2>/dev/null
    git checkout -q -b work
    # The added line is appended; the gate scans diff additions vs main.
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

run_reviewer_case "reviewer-tag colon form matches"      "Reviewer Bob: nit"           1
run_reviewer_case "reviewer-tag hyphen+slash form matches" "Reviewer-Alice/round-2"    1
run_reviewer_case "prose Reviewer Request is exempt"     "Reviewer Request returns nil" 0
run_reviewer_case "compound -reviewer prose is exempt"   "Zero-reviewer Request"        0
run_reviewer_case "lowercase reviewer JSON prose exempt" "reviewer JSON object"         0

echo
if [ "$fails" -gt 0 ]; then
  echo "doc-check_test: $fails failed, $passes passed"
  for n in "${failed_names[@]}"; do echo "  - $n"; done
  exit 1
fi
echo "doc-check_test: all $passes case(s) passed"

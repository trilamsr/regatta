#!/usr/bin/env bash
# check-tdd-redfirst.sh - assert TDD commit ORDERING, not just test presence.
#
# Drift this closes (MAY-261): a PR landed a new prod *.go and its
# co-located *_test.go in ONE commit, the body falsely claimed "red-first",
# and a confirm-reviewer accepted the false claim. check-tdd.sh checks test
# PRESENCE and a coarse first-commit-is-a-test heuristic that SKIPS
# single-commit PRs entirely (commit_count > 1 guard) — so test+impl in one
# commit sails through both. This gate removes reliance on a reviewer
# catching a false red-first claim.
#
# Rule (fires ONLY when a PR ADDS both a new prod *.go and its co-located
# new *_test.go in the same directory):
#   the commit that first ADDED the test file MUST be strictly EARLIER than
#   the commit that first ADDED the prod file. Same-commit (test+impl
#   together) and impl-first both fail.
#
# Out of scope (pass, by design — keep it simple, don't over-fire):
#   - no newly-added prod+co-located-test pair (edits to existing files,
#     test-only diffs, prod-only diffs)
#   - generated prod files ("Code generated" / "DO NOT EDIT" preamble)
#   - testdata/ fixtures
#
# Escape: `<!-- tdd-single-commit-justified: <reason ≥4 chars> -->` in body
#   (use when test+impl genuinely belong in one commit, e.g. a fixture and
#   the script it tests where the test cannot precede the script).
#
# Inputs (mirrors check-tdd.sh):
#   BASE_SHA, HEAD_SHA - HEAD_SHA names the PR tip; BASE_SHA is a HINT only.
#     When origin/main (or main) is reachable we recompute the live
#     merge-base and ignore BASE_SHA (GH snapshots a stale base when main
#     moves between PR events).
#   BODY               - PR body text for the escape parse.

set -euo pipefail

head="${HEAD_SHA:-HEAD}"
base=""
if git rev-parse --verify origin/main >/dev/null 2>&1; then
  base="$(git merge-base origin/main "$head" 2>/dev/null || echo "")"
elif git rev-parse --verify main >/dev/null 2>&1; then
  base="$(git merge-base main "$head" 2>/dev/null || echo "")"
fi
if [ -z "$base" ]; then
  base="${BASE_SHA:-}"
fi
if [ -z "$base" ]; then
  echo "check-tdd-redfirst: no base ref resolvable; nothing to check"
  exit 0
fi

if [ -n "${BODY:-}" ] && printf '%s' "$BODY" | grep -qE '<!--[[:space:]]*tdd-single-commit-justified:[[:space:]]*[^[:space:]-][^-]{3,}-->'; then
  echo "check-tdd-redfirst: tdd-single-commit-justified escape present; skipping"
  exit 0
fi

# Newly-ADDED files only: an edit to an existing prod/test file carries no
# red-first ordering claim, so the gate must not fire on it.
added_files=$(git diff --name-only --diff-filter=A "$base" "$head" 2>/dev/null || true)
if [ -z "$added_files" ]; then
  echo "check-tdd-redfirst: no newly-added files; nothing to check"
  exit 0
fi

prod_added=""
test_added=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case "$f" in
    *.go) ;;
    *) continue ;;
  esac
  case "$f" in
    */testdata/*|testdata/*) continue ;;
  esac
  # Read the file content once. `grep -q` / `head` close their pipe early; a
  # bare `git show | grep -q` would let `set -o pipefail` surface the upstream
  # SIGPIPE as a 141 failure inside this `set -e` loop. Buffering in a var
  # drains the producer fully, sidestepping the whole class.
  content=$(git show "$head:$f" 2>/dev/null || true)
  case "$f" in
    *_test.go)
      # Only a substantive test (real Test/Benchmark/Example func) counts as
      # the red half of a red-first pair.
      if printf '%s\n' "$content" | grep -qE '^func[[:space:]]+(Test|Benchmark|Example)[A-Z_]'; then
        test_added="$test_added $f"
      fi
      ;;
    *)
      # Generated code carries no hand-written test ordering.
      if printf '%s\n' "$content" | head -5 | grep -qE 'Code generated|DO NOT EDIT'; then
        continue
      fi
      prod_added="$prod_added $f"
      ;;
  esac
done <<< "$added_files"

prod_added=$(echo "$prod_added" | tr ' ' '\n' | sed '/^$/d')
test_added=$(echo "$test_added" | tr ' ' '\n' | sed '/^$/d')

if [ -z "$prod_added" ] || [ -z "$test_added" ]; then
  echo "check-tdd-redfirst: no newly-added prod+test pair; out of scope"
  exit 0
fi

# first_add_commit <path> -> the earliest commit in base..head that ADDED
# the file. --reverse lists oldest-first; --diff-filter=A restricts to the
# add. A file added, deleted, then re-added on the branch lists more than one
# add commit; we take the FIRST. `awk NR==1` (no `exit`) drains the whole
# pipe, so `set -o pipefail` cannot turn an early-closed-pipe SIGPIPE from a
# multi-line `git log` into a 141 exit (which `head -1` would).
first_add_commit() {
  git log --reverse --diff-filter=A --format=%H "$base..$head" -- "$1" 2>/dev/null | awk 'NR==1'
}

# Ordering uses ancestry, not timestamps (which tie / skew on rebase). Within
# a linear PR branch, the earlier commit is a strict ancestor of the later.
# bash 3.2 (macOS) safe: no associative arrays.
violations=""
while IFS= read -r pf; do
  [ -z "$pf" ] && continue
  pdir=$(dirname "$pf")
  # Co-located = a newly-added test in the SAME directory (same package).
  # Same-package is the only form that carries an unambiguous red-first
  # claim for THIS prod file; subtree matches are out of scope here.
  paired_test=""
  while IFS= read -r tf; do
    [ -z "$tf" ] && continue
    if [ "$(dirname "$tf")" = "$pdir" ]; then
      paired_test="$tf"
      break
    fi
  done <<< "$test_added"
  [ -z "$paired_test" ] && continue

  prod_commit=$(first_add_commit "$pf")
  test_commit=$(first_add_commit "$paired_test")
  if [ -z "$prod_commit" ] || [ -z "$test_commit" ]; then
    continue
  fi

  if [ "$test_commit" = "$prod_commit" ]; then
    violations="${violations}  $paired_test + $pf added in ONE commit ${prod_commit:0:12} (test+impl together)"$'\n'
  elif ! git merge-base --is-ancestor "$test_commit" "$prod_commit" 2>/dev/null; then
    # test_commit is NOT an ancestor of prod_commit -> the test did not land
    # first (impl-first, or unrelated branch topology).
    violations="${violations}  $pf (impl ${prod_commit:0:12}) added BEFORE its test $paired_test (${test_commit:0:12})"$'\n'
  fi
done <<< "$prod_added"

if [ -n "$violations" ]; then
  echo "check-tdd-redfirst: TDD red-first ordering violation per feedback_tdd_discipline." >&2
  echo "  A failing test MUST land in an EARLIER commit than the impl it covers." >&2
  printf '%s' "$violations" >&2
  echo >&2
  echo "Fix: split the test into its own earlier commit (rebase the test commit" >&2
  echo "  to the top of the branch), OR add to the PR body:" >&2
  echo "  <!-- tdd-single-commit-justified: <reason ≥4 chars> -->" >&2
  exit 1
fi

echo "check-tdd-redfirst: every newly-added prod+test pair landed test-first"

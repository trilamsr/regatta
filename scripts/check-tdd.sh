#!/usr/bin/env bash
# check-tdd.sh - test-first enforcement gate.
#
# Drift pattern this gate closes: production Go code added without a
# corresponding test addition in the same PR. TDD discipline is
# self-imposed today; this script makes the trail visible to CI so a
# reviewer cannot wave a PR through that skipped the RED/GREEN cycle.
#
# Rule:
#   For every non-test *.go file with added lines in the PR diff,
#   require AT LEAST ONE matching *_test.go file in the same PR diff
#   that also has added lines.
#
# "Matching" is conservative: the test file may live in the same
# package directory (foo.go + foo_test.go, or foo.go + bar_test.go in
# the same dir), or anywhere under the same internal/ or cmd/ top-level
# subtree. This avoids forcing one test file per source file while
# still requiring the PR to ship some test along with code.
#
# Exemptions (no test required):
#   1. PR only touches *_test.go (test-only PR).
#   2. Generated files: first 5 lines contain "Code generated".
#   3. PR body opts out with category [DOCS] / [CHORE] / [CI] in the
#      release-notes block (already enforced separately; we just read
#      the block for the category prefix).
#   4. Override label 'tdd-justified' set by maintainer (handled in
#      the GitHub Actions wrapper - this script does not see labels).
#
# Inputs:
#   BASE_SHA, HEAD_SHA - HEAD_SHA names the PR tip; BASE_SHA is a HINT
#   only. When origin/main (or main) is present we always recompute
#   `git merge-base <ref> HEAD` and ignore BASE_SHA. GitHub's
#   pull_request.base.sha snapshots at PR-open and only refreshes on
#   PR events (synchronize / edited / labeled); when main moves between
#   events the env var points at a pre-move base and the diff window
#   pulls in unrelated commits that landed on main, falsely tripping
#   the gate. Live merge-base resolution is GH-agnostic and matches
#   the local `make pre-push-check` path.
#   BODY               - PR body text for the category-opt-out parse.

set -euo pipefail

head="${HEAD_SHA:-HEAD}"
base=""
if git rev-parse --verify origin/main >/dev/null 2>&1; then
  base="$(git merge-base origin/main "$head" 2>/dev/null || echo "")"
elif git rev-parse --verify main >/dev/null 2>&1; then
  base="$(git merge-base main "$head" 2>/dev/null || echo "")"
fi
# Fallback when no main ref is reachable (shallow clones, detached
# fixtures): trust the BASE_SHA hint.
if [ -z "$base" ]; then
  base="${BASE_SHA:-}"
fi
if [ -z "$base" ]; then
  echo "check-tdd: no base ref resolvable; nothing to check"
  exit 0
fi

# Category opt-out via release-notes block.
if [ -n "${BODY:-}" ]; then
  category=$(printf '%s\n' "$BODY" | awk '
    /^```release-notes/ { capture=1; next }
    capture && /^```/    { exit }
    capture              { print }
  ' | grep -oE '\[[A-Z]+\]' | head -1 || true)
  case "$category" in
    "[DOCS]"|"[CHORE]"|"[CI]"|"[NONE]"|"[CHANGE]")
      # [CHANGE] covers structural reshape (moves, renames, contract
      # promotion); no new behavior to test, so the existing test
      # suite continues to falsify the moved code.
      echo "check-tdd: release-notes category $category - test-first not required"
      exit 0
      ;;
  esac
fi

# Collect every non-test *.go file that gained lines in this PR.
prod_added=""
test_added=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case "$f" in
    *.go) ;;
    *) continue ;;
  esac
  # testdata/ is the Go-toolchain-reserved fixture directory: the
  # compiler ignores it and `go test` does not load it as a package.
  # *.go files under it are fixture material, not production code.
  case "$f" in
    */testdata/*|testdata/*) continue ;;
  esac
  # Count lines added (starts with '+' but not '+++').
  added=$(git diff "$base" "$head" -- "$f" | grep -cE '^\+[^+]' || true)
  if [ "$added" -eq 0 ]; then
    continue
  fi
  # Generated-file skip: check the file content at HEAD for the
  # canonical "Code generated" preamble in the first 5 lines.
  if git show "$head:$f" 2>/dev/null | head -5 | grep -qE 'Code generated|DO NOT EDIT'; then
    continue
  fi
  case "$f" in
    *_test.go)
      # Substance check: a *_test.go that contains no `func Test...`
      # or `func Benchmark...` or `func Example...` declaration at HEAD
      # does not satisfy the gate. Bypass-by-empty-file is closed.
      if ! git show "$head:$f" 2>/dev/null | grep -qE '^func[[:space:]]+(Test|Benchmark|Example)[A-Z_]'; then
        continue
      fi
      test_added="$test_added $f"
      ;;
    *) prod_added="$prod_added $f" ;;
  esac
done < <(git diff --name-only "$base" "$head")

# If no production-code additions, nothing to check.
prod_added=$(echo "$prod_added" | tr ' ' '\n' | sed '/^$/d')
test_added=$(echo "$test_added" | tr ' ' '\n' | sed '/^$/d')
if [ -z "$prod_added" ]; then
  echo "check-tdd: no production *.go additions; test-first not required"
  exit 0
fi

# Production code present. Require at least one test file in the same
# top-level subtree.
# Build the set of test-file top-level subtrees (first path segment).
test_dirs=""
while IFS= read -r tf; do
  [ -z "$tf" ] && continue
  test_dirs="$test_dirs $(dirname "$tf")"
done <<< "$test_added"

failures=""
while IFS= read -r pf; do
  [ -z "$pf" ] && continue
  pdir=$(dirname "$pf")
  # Same dir? Walk up: a test file in any ancestor dir of pdir counts.
  found=0
  d="$pdir"
  while [ "$d" != "." ] && [ "$d" != "/" ]; do
    for td in $test_dirs; do
      if [ "$td" = "$d" ]; then
        found=1
        break
      fi
      # td under d? d under td? Either direction is a match.
      case "$td/" in "$d/"*) found=1; break;; esac
      case "$d/" in "$td/"*) found=1; break;; esac
    done
    [ "$found" -eq 1 ] && break
    d=$(dirname "$d")
  done
  # Cross-package symbol-match fallback (#1055): cmd/ wire files are
  # commonly exercised by internal/ unit tests, which the ancestor
  # walk cannot reach (cmd/ and internal/ share no ancestor below /).
  if [ "$found" -eq 0 ]; then
    symbols=$(git diff "$base" "$head" -- "$pf" \
      | awk '
          /^\+func[[:space:]]+\(/ {
            # method receiver: "+func (r *Recv) Method(...)"
            for (i=1; i<=NF; i++) if ($i ~ /^[A-Z]/) { sub(/\(.*/, "", $i); print $i; next }
            next
          }
          /^\+func[[:space:]]+[A-Z]/ {
            sym=$2; sub(/\(.*/, "", sym); print sym; next
          }
          /^\+type[[:space:]]+[A-Z]/  { print $2; next }
          /^\+var[[:space:]]+[A-Z]/   { print $2; next }
          /^\+const[[:space:]]+[A-Z]/ { print $2; next }
        ' \
      | sort -u)
    if [ -n "$symbols" ]; then
      while IFS= read -r tf; do
        [ -z "$tf" ] && continue
        for sym in $symbols; do
          if git show "$head:$tf" 2>/dev/null | grep -qw "$sym"; then
            found=1
            break 2
          fi
        done
      done <<< "$test_added"
    fi
  fi
  if [ "$found" -eq 0 ]; then
    failures="$failures\n  $pf"
  fi
done <<< "$prod_added"

if [ -n "$failures" ]; then
  echo "check-tdd: production *.go added without a matching *_test.go in this PR:"
  printf '%b\n' "$failures"
  echo
  echo "TDD discipline: write the test first, watch it fail, then write the code."
  echo "If this PR is a deliberate exception, set the label 'tdd-justified'"
  echo "or categorize the release-notes block as [DOCS] / [CHORE] / [CI]."
  exit 1
fi

echo "check-tdd: every production *.go addition has a matching *_test.go in the same PR"

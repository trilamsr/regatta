#!/usr/bin/env bash
# Black-box smoke tests for worktree-gc.sh. Stands up a throwaway origin +
# clone, creates two worktrees on temp branches, merges one into main,
# asserts dry-run lists exactly the merged candidate and --apply removes
# only that one. Primary checkout, cwd, and main-branch worktrees must
# stay untouched in both modes.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$script_dir/worktree-gc.sh"

failed=0
pass=0

# setup_fixture: bare origin + main worktree + 2 feature worktrees, one
# whose branch is merged into origin/main, one whose branch is not. Sets
# globals tmp + work_dir; caller must `cd "$work_dir"` before invoking
# the script under test (the script skips cwd, so cases pick whichever
# checkout they want to run from).
setup_fixture() {
  tmp=$(mktemp -d)
  cd "$tmp"

  git init -q --bare origin.git
  git clone -q origin.git work
  cd work
  git config user.email t@t
  git config user.name t
  git commit -q --allow-empty -m base
  git branch -M main
  git push -q -u origin main

  # Merged feature branch: commit, push, fast-forward into main, push main.
  git checkout -q -b feature-merged
  git commit -q --allow-empty -m merged-work
  git push -q -u origin feature-merged
  git checkout -q main
  git merge -q --ff-only feature-merged
  git push -q origin main

  # Unmerged feature branch: commit only; never merged into main.
  git checkout -q -b feature-unmerged
  git commit -q --allow-empty -m unmerged-work
  git push -q -u origin feature-unmerged
  git checkout -q main

  # Create worktrees for both feature branches.
  git worktree add -q "$tmp/wt-merged" feature-merged
  git worktree add -q "$tmp/wt-unmerged" feature-unmerged

  work_dir="$tmp/work"
}

teardown_fixture() {
  cd / 2>/dev/null || true
  # Best-effort cleanup; locked worktrees may need force-twice.
  (cd "$work_dir" 2>/dev/null && git worktree list --porcelain 2>/dev/null \
    | awk '/^worktree / && $2 != "'"$work_dir"'" {print $2}' \
    | xargs -I{} git worktree remove --force --force {} 2>/dev/null) || true
  rm -rf "$tmp"
}

assert_contains() {
  local haystack="$1" needle="$2" name="$3"
  if echo "$haystack" | grep -qE "$needle"; then
    return 0
  fi
  echo "FAIL: $name"
  echo "  expected stdout to match: $needle"
  echo "  got:"
  echo "$haystack" | sed 's/^/    /'
  failed=$((failed+1))
  return 1
}

assert_absent() {
  local haystack="$1" needle="$2" name="$3"
  if echo "$haystack" | grep -qE "$needle"; then
    echo "FAIL: $name"
    echo "  expected stdout NOT to match: $needle"
    echo "  got:"
    echo "$haystack" | sed 's/^/    /'
    failed=$((failed+1))
    return 1
  fi
  return 0
}

# Case 1: dry-run lists the merged worktree only; neither worktree removed.
setup_fixture
cd "$work_dir"
out=$(bash "$script" 2>&1) && got_exit=0 || got_exit=$?
ok=1
assert_contains "$out" "wt-merged" "dry-run lists merged worktree" || ok=0
assert_absent   "$out" "wt-unmerged.*remove" "dry-run does not list unmerged for removal" || ok=0
[ "$got_exit" = "0" ] || { echo "FAIL: dry-run exit=$got_exit want 0"; failed=$((failed+1)); ok=0; }
[ -d "$tmp/wt-merged" ] || { echo "FAIL: dry-run removed wt-merged"; failed=$((failed+1)); ok=0; }
[ -d "$tmp/wt-unmerged" ] || { echo "FAIL: dry-run removed wt-unmerged"; failed=$((failed+1)); ok=0; }
[ "$ok" = "1" ] && { echo "PASS: dry-run lists merged only, removes nothing"; pass=$((pass+1)); }
teardown_fixture

# Case 2: --apply removes the merged worktree, keeps the unmerged one.
setup_fixture
cd "$work_dir"
out=$(bash "$script" --apply 2>&1) && got_exit=0 || got_exit=$?
ok=1
[ "$got_exit" = "0" ] || { echo "FAIL: --apply exit=$got_exit want 0"; echo "$out"; failed=$((failed+1)); ok=0; }
[ ! -d "$tmp/wt-merged" ] || { echo "FAIL: --apply did not remove wt-merged"; failed=$((failed+1)); ok=0; }
[ -d "$tmp/wt-unmerged" ] || { echo "FAIL: --apply wrongly removed wt-unmerged"; failed=$((failed+1)); ok=0; }
[ "$ok" = "1" ] && { echo "PASS: --apply removes merged, keeps unmerged"; pass=$((pass+1)); }
teardown_fixture

# Case 3: cwd must be skipped even if its branch is merged. Run script
# from inside wt-merged itself; it must NOT remove its own checkout.
setup_fixture
cd "$tmp/wt-merged"
out=$(bash "$script" --apply 2>&1) && got_exit=0 || got_exit=$?
ok=1
[ -d "$tmp/wt-merged" ] || { echo "FAIL: --apply removed cwd worktree"; failed=$((failed+1)); ok=0; }
[ "$ok" = "1" ] && { echo "PASS: --apply skips cwd worktree"; pass=$((pass+1)); }
teardown_fixture

# Case 4: main worktree never removed even if --apply.
setup_fixture
cd "$work_dir"
out=$(bash "$script" --apply 2>&1) && got_exit=0 || got_exit=$?
ok=1
[ -d "$work_dir" ] || { echo "FAIL: --apply removed main worktree"; failed=$((failed+1)); ok=0; }
[ "$ok" = "1" ] && { echo "PASS: --apply preserves main worktree"; pass=$((pass+1)); }
teardown_fixture

# Case 5: detached-HEAD worktree on a merged commit must be skipped, NOT
# flagged for removal. Operator-explicit detach signals "keep this around"
# regardless of ancestry. Reviewer finding #2 on PR #716.
setup_fixture
cd "$work_dir"
merged_sha=$(git rev-parse feature-merged)
git worktree add -q --detach "$tmp/wt-detached" "$merged_sha"
out=$(bash "$script" 2>&1) && got_exit=0 || got_exit=$?
ok=1
assert_absent "$out" "wt-detached.*remove" "dry-run does not flag detached worktree" || ok=0
assert_contains "$out" "kept.*wt-detached.*\(detached\)" "dry-run reports detached worktree with reason=detached" || ok=0
out_apply=$(bash "$script" --apply 2>&1) && got_exit=0 || got_exit=$?
[ -d "$tmp/wt-detached" ] || { echo "FAIL: --apply removed detached worktree"; failed=$((failed+1)); ok=0; }
[ "$ok" = "1" ] && { echo "PASS: detached-HEAD worktree on merged commit is skipped"; pass=$((pass+1)); }
teardown_fixture

# Case 6: --apply --force removes a locked merged worktree via force-twice
# fallback. Reviewer finding #8 on PR #716.
setup_fixture
cd "$work_dir"
git worktree lock "$tmp/wt-merged" 2>/dev/null || true
out=$(bash "$script" --apply --force 2>&1) && got_exit=0 || got_exit=$?
ok=1
[ "$got_exit" = "0" ] || { echo "FAIL: --apply --force exit=$got_exit want 0"; echo "$out"; failed=$((failed+1)); ok=0; }
[ ! -d "$tmp/wt-merged" ] || { echo "FAIL: --apply --force did not remove locked merged worktree"; failed=$((failed+1)); ok=0; }
assert_contains "$out" "wt-merged.*force-twice" "locked removal uses force-twice path" || ok=0
[ "$ok" = "1" ] && { echo "PASS: --apply --force removes locked merged worktree"; pass=$((pass+1)); }
teardown_fixture

echo
echo "worktree-gc_test.sh: $pass passed, $failed failed"
[ "$failed" -eq 0 ]

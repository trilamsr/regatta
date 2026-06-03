#!/usr/bin/env bash
# Black-box smoke tests for cleanup-merged-branches.sh's three passes.
# Each case stands up a throwaway origin + clone, primes the gh stub via
# per-test fixture files, then asserts the right branches are flagged.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$script_dir/cleanup-merged-branches.sh"

failed=0
pass=0

run_case() {
  local name="$1"; shift
  local want_match="$1"; shift   # regex stdout must match (or empty)
  local want_absent="$1"; shift  # regex stdout must NOT match (or empty)
  local setup="$1"; shift        # bash function name

  _run "$name" "$want_match" "$want_absent" "$setup" 0 --dry-run
}

# Extended runner: caller controls expected exit code and script args.
# Lets fetch-failure cases assert exit != 0 without changing the legacy runner.
_run() {
  local name="$1"; shift
  local want_match="$1"; shift
  local want_absent="$1"; shift
  local setup="$1"; shift
  local want_exit="$1"; shift
  # Remaining args ($@) are passed to the script under test.

  tmp=$(mktemp -d)
  pushd "$tmp" >/dev/null

  # Bare origin repo.
  git init -q --bare origin.git
  git clone -q origin.git work
  pushd work >/dev/null
  git config user.email t@t
  git config user.name t
  git commit -q --allow-empty -m base
  git branch -M main
  git push -q -u origin main

  # Stub gh. Emits pre-extracted values (mimicking real gh's server-side --jq).
  # Pass-1 fixture: $tmp/gh-merged-headrefs (newline-separated headRefNames).
  # Pass-2 fixture: $tmp/gh-pr-state-<branch> (MERGED/CLOSED/OPEN; absent => "").
  mkdir -p "$tmp/bin"
  cat > "$tmp/bin/gh" <<EOF
#!/usr/bin/env bash
search_branch=""
for ((i=1; i<=\$#; i++)); do
  if [ "\${!i}" = "--search" ]; then
    j=\$((i+1))
    search_branch="\${!j#head:}"
    break
  fi
done
if [ -n "\$search_branch" ]; then
  fixture="$tmp/gh-pr-state-\$search_branch"
  if [ -f "\$fixture" ]; then
    cat "\$fixture"
  fi
  # Absent fixture => empty stdout, matching --jq '.[0].state // ""' on [].
else
  fixture="$tmp/gh-merged-headrefs"
  if [ -f "\$fixture" ]; then
    cat "\$fixture"
  fi
  # Absent fixture => empty stdout, matching --jq '.[].headRefName' on [].
fi
EOF
  chmod +x "$tmp/bin/gh"

  "$setup"

  out=$(PATH="$tmp/bin:$PATH" bash "$script" "$@" 2>&1) && got_exit=0 || got_exit=$?

  ok=1
  if [ "$got_exit" != "$want_exit" ]; then
    echo "FAIL $name: exit=$got_exit want $want_exit"
    echo "  output: $out"
    ok=0
  fi
  if [ -n "$want_match" ] && ! echo "$out" | grep -Eq "$want_match"; then
    echo "FAIL $name: stdout missing /$want_match/"
    echo "  output: $out"
    ok=0
  fi
  if [ -n "$want_absent" ] && echo "$out" | grep -Eq "$want_absent"; then
    echo "FAIL $name: stdout should not match /$want_absent/"
    echo "  output: $out"
    ok=0
  fi

  # Setups may set verify_branch_still_exists to assert a branch
  # survived (catches false-positive deletions that a stdout grep alone
  # would miss).
  if [ "${verify_branch_still_exists:-}" != "" ]; then
    if ! git rev-parse --verify "refs/heads/$verify_branch_still_exists" >/dev/null 2>&1; then
      echo "FAIL $name: branch $verify_branch_still_exists missing (should be preserved)"
      ok=0
    fi
  fi

  if [ $ok -eq 1 ]; then
    pass=$((pass + 1))
    echo "PASS $name"
  else
    failed=$((failed + 1))
  fi

  popd >/dev/null  # leave work
  popd >/dev/null  # leave tmp
  rm -rf "$tmp"
  unset verify_branch_still_exists
}

# Local branch `foo` pushed as `bar`, bar merged-and-pruned on origin.
# Pass 1 sees no PR (gh stub returns []); pass 2 must flag foo as
# upstream-gone because origin/bar no longer exists.
case_gone_upstream_detected() {
  # Create local branch foo, push as bar with tracking.
  git checkout -q -b foo
  git commit -q --allow-empty -m foo-work
  git push -q -u origin foo:bar
  # Merge bar into main on origin, then delete bar.
  pushd ../origin.git >/dev/null
  git update-ref refs/heads/main refs/heads/bar
  git update-ref -d refs/heads/bar
  popd >/dev/null
  # Bring local tracking in sync with origin (prune dead remote refs).
  git fetch -q --prune
  # Back to main so foo isn't the current branch.
  git checkout -q main
}

# Branch with upstream still present must NOT be touched.
case_live_upstream_skipped() {
  git checkout -q -b alive
  git commit -q --allow-empty -m alive-work
  git push -q -u origin alive
  git checkout -q main
  git fetch -q --prune
}

# Branch with no upstream at all (never pushed) must NOT be touched —
# upstream:track is empty, not [gone].
case_no_upstream_skipped() {
  git checkout -q -b local-only
  git commit -q --allow-empty -m local-only
  git checkout -q main
}

# Current branch must never be deleted even if upstream is gone.
case_current_branch_protected() {
  git checkout -q -b stuck
  git commit -q --allow-empty -m stuck
  git push -q -u origin stuck:remote-name
  pushd ../origin.git >/dev/null
  git update-ref -d refs/heads/remote-name
  popd >/dev/null
  git fetch -q --prune
  # Stay on stuck; do NOT switch to main.
}

# main must never be deleted even if its upstream were gone (defensive).
case_main_protected() {
  # Delete origin's main ref to simulate gone upstream for main.
  pushd ../origin.git >/dev/null
  git update-ref -d refs/heads/main
  popd >/dev/null
  git fetch -q --prune
}

# Branch whose upstream was deleted WITHOUT merging into main must be
# preserved (e.g. PR closed-without-merge or remote ref manually pruned).
case_unmerged_gone_preserved() {
  git checkout -q -b lost-work
  git commit -q --allow-empty -m never-merged
  git push -q -u origin lost-work:remote-lost
  pushd ../origin.git >/dev/null
  git update-ref -d refs/heads/remote-lost
  popd >/dev/null
  git fetch -q --prune
  git checkout -q main
}

run_case gone_upstream_detected   'would delete branch foo \(upstream gone\)' ''                                 case_gone_upstream_detected
run_case live_upstream_skipped    ''                                          'would delete branch alive'        case_live_upstream_skipped
run_case no_upstream_skipped      ''                                          'would delete branch local-only'   case_no_upstream_skipped
run_case current_branch_protected ''                                          'would delete branch stuck'        case_current_branch_protected
run_case main_protected           ''                                          'would delete branch main'         case_main_protected
run_case unmerged_gone_preserved  'skip lost-work \(upstream gone but not merged' 'would delete branch lost-work' case_unmerged_gone_preserved

# Pass-2 fetch fails (unreachable origin). Default behavior MUST abort
# non-zero so a stale [gone] marker can't drive a deletion against a
# stale origin/main snapshot. Also asserts the gone-marker branch is
# preserved (no deletion) and the error message names the failure.
case_fetch_failure_aborts() {
  git checkout -q -b foo
  git commit -q --allow-empty -m foo-work
  git push -q -u origin foo:bar
  pushd ../origin.git >/dev/null
  git update-ref refs/heads/main refs/heads/bar
  git update-ref -d refs/heads/bar
  popd >/dev/null
  git fetch -q --prune
  git checkout -q main
  # Break origin AFTER local [gone] marker is set: subsequent fetch fails.
  git remote set-url origin "$tmp/does-not-exist.git"
  # Real script (not --dry-run) so the failure path runs unconditionally.
  verify_branch_still_exists=foo
}

# --allow-stale converts the fetch failure into a warning + skipped
# pass-2. Pass 1 still completes (exit 0). The gone-marker branch is
# preserved because pass 2 never runs.
case_fetch_failure_allow_stale() {
  git checkout -q -b foo
  git commit -q --allow-empty -m foo-work
  git push -q -u origin foo:bar
  pushd ../origin.git >/dev/null
  git update-ref refs/heads/main refs/heads/bar
  git update-ref -d refs/heads/bar
  popd >/dev/null
  git fetch -q --prune
  git checkout -q main
  git remote set-url origin "$tmp/does-not-exist.git"
  verify_branch_still_exists=foo
}

# Repo whose default branch is `trunk` (not `main`). Two branches:
#   - foo:  merged into trunk via PR-style update — must be deleted.
#   - keep: never merged into trunk — must be preserved.
# A script that hard-codes `main` looks up an empty main_sha, skips the
# ancestry check, and falsely deletes `keep`. The fix discovers trunk
# via origin/HEAD and runs the ancestry check against origin/trunk.
case_non_main_default_branch() {
  # Tear down the default-main setup the runner created and rebuild
  # the work clone with trunk as origin/HEAD.
  popd >/dev/null  # leave work
  rm -rf work
  pushd origin.git >/dev/null
  git symbolic-ref HEAD refs/heads/trunk
  git update-ref refs/heads/trunk refs/heads/main
  git update-ref -d refs/heads/main
  popd >/dev/null
  git clone -q origin.git work
  pushd work >/dev/null
  git config user.email t@t
  git config user.name t
  # foo: merged-and-pruned into trunk.
  git checkout -q -b foo
  git commit -q --allow-empty -m foo-work
  git push -q -u origin foo:bar
  pushd ../origin.git >/dev/null
  git update-ref refs/heads/trunk refs/heads/bar
  git update-ref -d refs/heads/bar
  popd >/dev/null
  # keep: gone upstream but never merged into trunk.
  git checkout -q trunk
  git pull -q --ff-only
  git checkout -q -b keep
  git commit -q --allow-empty -m unmerged-work
  git push -q -u origin keep:remote-keep
  pushd ../origin.git >/dev/null
  git update-ref -d refs/heads/remote-keep
  popd >/dev/null
  git fetch -q --prune
  git checkout -q trunk
}

# --dry-run honored in pass 2: gone-upstream candidate is logged but
# branch survives (test #1 covers the log; this one verifies the
# filesystem side and re-asserts dry-run end-to-end).
case_dry_run_no_deletion() {
  git checkout -q -b foo
  git commit -q --allow-empty -m foo-work
  git push -q -u origin foo:bar
  pushd ../origin.git >/dev/null
  git update-ref refs/heads/main refs/heads/bar
  git update-ref -d refs/heads/bar
  popd >/dev/null
  git fetch -q --prune
  git checkout -q main
  verify_branch_still_exists=foo
}

_run fetch_failure_default_aborts             'fetch failed' 'would delete branch foo' case_fetch_failure_aborts             1
_run fetch_failure_allow_stale_skips_second_pass 'allow-stale' 'would delete branch foo' case_fetch_failure_allow_stale       0 --dry-run --allow-stale
_run non_main_default_branch                  'would delete branch foo \(upstream gone\)' 'would delete branch keep' case_non_main_default_branch       0 --dry-run
_run dry_run_second_pass_no_deletion          'would delete branch foo \(upstream gone\)' '' case_dry_run_no_deletion           0 --dry-run

# Squash-merge: pass-2 probe returns MERGED; pass 1 left empty.
# verify_branch_still_exists doubles as a dry-run filesystem assertion.
case_squash_merged_detected() {
  git checkout -q -b squashed
  git commit -q --allow-empty -m squash-work
  git push -q -u origin squashed
  git checkout -q main
  echo 'MERGED' > "$tmp/gh-pr-state-squashed"
  verify_branch_still_exists=squashed
}

# CLOSED-state preserved by default.
case_closed_default_preserved() {
  git checkout -q -b abandoned
  git commit -q --allow-empty -m abandoned-work
  git push -q -u origin abandoned
  git checkout -q main
  echo 'CLOSED' > "$tmp/gh-pr-state-abandoned"
  verify_branch_still_exists=abandoned
}

# CLOSED-state deleted under --include-closed.
case_closed_include_closed_deletes() {
  git checkout -q -b abandoned
  git commit -q --allow-empty -m abandoned-work
  git push -q -u origin abandoned
  git checkout -q main
  echo 'CLOSED' > "$tmp/gh-pr-state-abandoned"
}

# OPEN-state preserved.
case_open_pr_preserved() {
  git checkout -q -b in-review
  git commit -q --allow-empty -m wip
  git push -q -u origin in-review
  git checkout -q main
  echo 'OPEN' > "$tmp/gh-pr-state-in-review"
  verify_branch_still_exists=in-review
}

# No-PR (absent fixture => state=""): preserved.
case_no_pr_preserved() {
  git checkout -q -b drafted
  git commit -q --allow-empty -m draft
  git checkout -q main
  verify_branch_still_exists=drafted
}

# Squash-merge with pinned .claude/worktrees/ worktree — pass 2 must
# remove the worktree before the branch.
case_squash_merged_with_worktree() {
  git checkout -q -b squash-wt
  git commit -q --allow-empty -m wt-work
  git push -q -u origin squash-wt
  git checkout -q main
  mkdir -p .claude/worktrees
  git worktree add -q .claude/worktrees/squash-wt squash-wt
  echo 'MERGED' > "$tmp/gh-pr-state-squash-wt"
}

_run squash_merged_detected                   'would delete branch squashed \(PR merged\)' '' case_squash_merged_detected   0 --dry-run
_run closed_default_preserved                 ''                                          'would delete branch abandoned' case_closed_default_preserved 0 --dry-run
_run closed_include_closed_deletes            'would delete branch abandoned \(PR closed' '' case_closed_include_closed_deletes 0 --dry-run --include-closed
_run open_pr_preserved                        ''                                          'would delete branch in-review' case_open_pr_preserved        0 --dry-run
_run no_pr_preserved                          ''                                          'would delete branch drafted'  case_no_pr_preserved          0 --dry-run
_run squash_merged_with_worktree_dry_run      'would remove worktree.*\.claude/worktrees/squash-wt' '' case_squash_merged_with_worktree 0 --dry-run

echo
echo "summary: $pass passed, $failed failed"
exit $failed

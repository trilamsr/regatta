#!/usr/bin/env bash
# Delete local branches + worktrees whose PRs have merged. Idempotent.
# Usage: bash scripts/cleanup-merged-branches.sh [--dry-run]
#
# Two passes:
#   1. PR-merged pass: branches whose name matches a merged PR's headRefName.
#   2. Gone-upstream pass: branches whose tracked upstream was deleted
#      (git's [gone] marker). Catches the case where the local branch
#      name differs from the pushed headRefName (e.g. `git push origin
#      foo:bar`, or worktree-named tracking) so the PR-merged pass alone
#      never recognizes the local branch. See issue #111.
#
# Smoke test: scripts/cleanup-merged-branches_test.sh.

set -euo pipefail

dry=0
[ "${1:-}" = "--dry-run" ] && dry=1
do_it() { [ $dry -eq 1 ] && echo "would $*" || { echo "$*"; eval "${*#*: }"; }; }

current=$(git rev-parse --abbrev-ref HEAD)

# Pass 1 — match merged PRs by headRefName.
merged=$(gh pr list --state merged --limit 200 --json headRefName --jq '.[].headRefName' 2>/dev/null || true)

for b in $merged; do
  [ "$b" = "main" ] && continue
  [ "$b" = "$current" ] && continue
  git rev-parse --verify "refs/heads/$b" >/dev/null 2>&1 || continue

  # Remove any worktree pinning the branch before deleting the branch.
  for wt in $(git worktree list --porcelain | awk -v B="refs/heads/$b" '
    /^worktree / {p=$2} /^branch / && $2==B {print p}'); do
    case "$wt" in *.claude/worktrees/*)
      do_it "remove worktree $wt: git worktree remove --force '$wt'" ;;
    esac
  done

  do_it "delete branch $b: git branch -D '$b'"
done

# Pass 2 — branches whose upstream is gone (PR merged + remote branch pruned).
# `%(upstream:track)` emits "[gone]" when the upstream ref is unknown. We
# prune first so the marker reflects the remote's current state; without
# the prune, recently-deleted upstreams stay invisible and pass 2 no-ops.
# Then we double-check each candidate is fully merged into main before
# deletion (defense-in-depth on top of git branch -d's own check), so a
# local foo whose upstream `bar` was deleted unmerged is left alone.
git fetch --prune --quiet 2>/dev/null || true

gone=$(git for-each-ref --format='%(refname:short) %(upstream:track)' refs/heads/ \
  | awk '$2=="[gone]" {print $1}')

main_sha=$(git rev-parse --verify origin/main 2>/dev/null \
  || git rev-parse --verify main 2>/dev/null || true)

for b in $gone; do
  [ "$b" = "main" ] && continue
  [ "$b" = "$current" ] && continue

  # Only delete branches whose tip is reachable from main — guards against
  # an upstream that was deleted *before* the PR landed (e.g. PR closed
  # without merge, force-push to a different ref, manual remote cleanup).
  if [ -n "$main_sha" ] && ! git merge-base --is-ancestor "refs/heads/$b" "$main_sha" 2>/dev/null; then
    echo "skip $b (upstream gone but not merged into main; preserve local work)"
    continue
  fi

  for wt in $(git worktree list --porcelain | awk -v B="refs/heads/$b" '
    /^worktree / {p=$2} /^branch / && $2==B {print p}'); do
    case "$wt" in *.claude/worktrees/*)
      do_it "remove worktree $wt: git worktree remove --force '$wt'" ;;
    esac
  done

  # -d (not -D) refuses unmerged branches as a final guard.
  do_it "delete branch $b (upstream gone): git branch -d '$b'"
done

git worktree prune

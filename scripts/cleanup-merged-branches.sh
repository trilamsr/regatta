#!/usr/bin/env bash
# Delete local branches + worktrees whose PRs have merged. Idempotent.
# Usage: bash scripts/cleanup-merged-branches.sh [--dry-run]

set -euo pipefail

dry=0
[ "${1:-}" = "--dry-run" ] && dry=1
do_it() { [ $dry -eq 1 ] && echo "would $*" || { echo "$*"; eval "${*#*: }"; }; }

current=$(git rev-parse --abbrev-ref HEAD)
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

git worktree prune

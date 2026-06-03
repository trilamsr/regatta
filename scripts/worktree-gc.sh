#!/usr/bin/env bash
# Remove worktrees whose branch is fully merged into origin/main. Dry-run
# by default — destructive flag --apply is required to actually remove.
# Default safety because operator friction from "worktree confused / branch
# locked" hit 8 times in one day with 11 stale worktrees accumulated on the
# operator machine (claude-mem memory finding 2026-06-03).
#
# Usage:
#   scripts/worktree-gc.sh           # dry-run; list worktrees that would be removed.
#   scripts/worktree-gc.sh --apply   # actually remove.
#   scripts/worktree-gc.sh --apply --force
#                                    # also retry locked worktrees with --force --force.
#
# Skips: primary checkout, cwd, any worktree on `main`. Branch-merged check
# uses `git merge-base --is-ancestor <wt-head> origin/<default-branch>`, so
# squash-merges and fast-forwards both qualify.
#
# Exit codes: 0 in dry-run always; 0 on clean --apply; non-zero only if
# --apply failed to remove a flagged candidate.
#
# Smoke test: scripts/worktree-gc_test.sh.

set -euo pipefail

apply=0
force=0
for arg in "$@"; do
  case "$arg" in
    --apply) apply=1 ;;
    --force) force=1 ;;
    -h|--help)
      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

# Resolve default branch via origin/HEAD; fall back to main. Hard-coding
# main would silently no-op the ancestry check on repos using trunk/master.
default_branch=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null \
  | sed 's@^origin/@@' || true)
[ -z "$default_branch" ] && default_branch=main

# Refresh origin's view of the default branch. Without this, a stale ref
# would let a candidate look "merged" when its remote already moved on.
git fetch -q origin "$default_branch" 2>/dev/null || true

cwd=$(pwd -P 2>/dev/null || pwd)

# Parse `git worktree list --porcelain` into newline-separated
# "<path>\t<branch>\t<head>" records. Empty branch = detached HEAD.
records=$(git worktree list --porcelain | awk '
  /^worktree / { wt=$2; head=""; branch=""; next }
  /^HEAD /     { head=$2; next }
  /^branch /   { branch=$2; next }
  /^bare$/     { next }
  /^detached$/ { next }
  /^$/         { if (wt != "") print wt "\t" branch "\t" head; wt=""; head=""; branch="" }
  END          { if (wt != "") print wt "\t" branch "\t" head }
')

# Primary = first worktree in the porcelain output. Skip unconditionally.
primary=$(echo "$records" | head -n1 | cut -f1)

kept=0
removed=0
errored=0
candidates=()

while IFS=$'\t' read -r wt branch head; do
  [ -z "$wt" ] && continue

  reason=""
  if [ "$wt" = "$primary" ]; then
    reason="primary"
  elif [ "$wt" = "$cwd" ]; then
    reason="cwd"
  elif [ "$branch" = "refs/heads/$default_branch" ]; then
    reason="on $default_branch"
  elif [ -z "$head" ]; then
    reason="no HEAD resolved"
  else
    # is-ancestor: 0 = ancestor (merged), 1 = not. Other = error.
    if git merge-base --is-ancestor "$head" "refs/remotes/origin/$default_branch" 2>/dev/null; then
      candidates+=("$wt")
      continue
    fi
    reason="unmerged"
  fi

  printf 'kept    %s\t(%s)\n' "$wt" "$reason"
  kept=$((kept+1))
done <<< "$records"

for wt in "${candidates[@]:-}"; do
  [ -z "$wt" ] && continue
  if [ "$apply" -eq 0 ]; then
    printf 'would remove %s\n' "$wt"
    removed=$((removed+1))
    continue
  fi
  if git worktree remove --force "$wt" 2>/dev/null; then
    printf 'removed %s\n' "$wt"
    removed=$((removed+1))
    continue
  fi
  if [ "$force" -eq 1 ] && git worktree remove --force --force "$wt" 2>/dev/null; then
    printf 'removed %s (force-twice)\n' "$wt"
    removed=$((removed+1))
    continue
  fi
  printf 'ERROR  %s\t(remove failed; retry with --force)\n' "$wt" >&2
  errored=$((errored+1))
done

printf '\nsummary: kept=%d removed=%d errored=%d (mode=%s)\n' \
  "$kept" "$removed" "$errored" "$([ "$apply" -eq 1 ] && echo apply || echo dry-run)"

if [ "$apply" -eq 1 ] && [ "$errored" -gt 0 ]; then
  exit 1
fi
exit 0

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
# is two-stage:
#   1. Ancestry: `git merge-base --is-ancestor <head> origin/<default-branch>`
#      catches fast-forwards + true merges (branch tip reachable from main).
#   2. Patch-equivalence: `git cherry origin/<default-branch> <branch>` catches
#      squash-merges and rebases — `-` prefix means the commit is already on
#      main as a different SHA. Branch qualifies when every cherry line is `-`
#      (or output is empty: zero unique commits ahead of main). Fix for #719:
#      ancestry-only missed 7 squash-merged worktrees on a 11-worktree dogfood.
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
# "<path>|<branch>|<head>" records. Empty branch = detached HEAD or bare.
# `|` (non-whitespace) delimiter is load-bearing: bash `read` with IFS=$'\t'
# collapses adjacent tabs, dropping the empty branch field and shifting head
# into branch's slot for detached worktrees.
records=$(git worktree list --porcelain | awk '
  /^worktree /  { wt=$2; head=""; branch=""; next }
  /^HEAD /      { head=$2; next }
  /^branch /    { branch=$2; next }
  /^bare$/      { wt=""; head=""; branch=""; next }
  /^detached$/  { next }
  /^$/          { if (wt != "") print wt "|" branch "|" head; wt=""; head=""; branch="" }
  END           { if (wt != "") print wt "|" branch "|" head }
')

# Primary = first worktree in the porcelain output. Skip unconditionally.
primary=$(echo "$records" | head -n1 | cut -d'|' -f1)

kept=0
removed=0
errored=0
candidates=()

while IFS='|' read -r wt branch head; do
  [ -z "$wt" ] && continue

  reason=""
  if [ "$wt" = "$primary" ]; then
    reason="primary"
  elif [ "$wt" = "$cwd" ]; then
    reason="cwd"
  elif [ "$branch" = "refs/heads/$default_branch" ]; then
    reason="on $default_branch"
  elif [ -z "$branch" ]; then
    # Detached HEAD: operator-explicit detach signals "keep this around"
    # even if the commit is an ancestor of origin/main.
    reason="detached"
  elif [ -z "$head" ]; then
    reason="no HEAD resolved"
  else
    # is-ancestor: 0 = ancestor (merged), 1 = not. Other = error.
    if git merge-base --is-ancestor "$head" "refs/remotes/origin/$default_branch" 2>/dev/null; then
      candidates+=("$wt")
      continue
    fi
    # Squash-merge / rebase fallback: `git cherry` compares patch-id, so a
    # commit re-applied as a different SHA on main still shows as `-`. Branch
    # qualifies only when there is no `+` line (every commit is on main).
    # Empty output (zero commits ahead) also qualifies. `branch` is `refs/heads/...`;
    # cherry accepts the full ref directly.
    if cherry_out=$(git cherry "refs/remotes/origin/$default_branch" "$branch" 2>/dev/null) \
        && ! grep -q '^+' <<<"$cherry_out"; then
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

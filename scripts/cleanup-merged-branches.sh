#!/usr/bin/env bash
# Delete local branches + worktrees whose PRs have merged. Idempotent.
# Usage: bash scripts/cleanup-merged-branches.sh [--dry-run] [--allow-stale] [--include-closed]
#
# Three passes:
#   1. PR-merged batch pass: branches whose name matches a merged PR's
#      headRefName (`gh pr list --state merged --limit 200`).
#   2. Per-branch PR-state probe: for any surviving local branch, query
#      `gh pr list --search "head:$b" --state all` and delete if the PR
#      is MERGED. Catches squash-merge cases where origin's branch ref
#      was not pruned (so pass 3's [gone] marker never fires) and where
#      `--limit 200` truncated the batch in pass 1. See feedback_root_cause.
#   3. Gone-upstream pass: branches whose tracked upstream was deleted
#      (git's [gone] marker). Catches the case where the local branch
#      name differs from the pushed headRefName (e.g. `git push origin
#      foo:bar`, or worktree-named tracking) so the PR-merged pass alone
#      never recognizes the local branch. See issue #111.
#
# Flags:
#   --dry-run         Print intended actions; perform none.
#   --allow-stale     On fetch failure (network/auth/proxy), warn and skip
#                     pass 3 instead of aborting. Passes 1+2 still run.
#                     Without this flag, fetch failure aborts non-zero so
#                     stale [gone] markers cannot drive deletions against
#                     a stale origin/HEAD snapshot. See issue #122.
#   --include-closed  Also delete branches whose PR state is CLOSED (default:
#                     MERGED only). Covers the operator-merge-then-close
#                     pattern. Off by default — CLOSED can mean abandoned
#                     work the operator still wants locally.
#
# Smoke test: scripts/cleanup-merged-branches_test.sh.

set -euo pipefail

dry=0
allow_stale=0
include_closed=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry=1 ;;
    --allow-stale) allow_stale=1 ;;
    --include-closed) include_closed=1 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done
do_it() { [ $dry -eq 1 ] && echo "would $*" || { echo "$*"; eval "${*#*: }"; }; }

current=$(git rev-parse --abbrev-ref HEAD)

# Detect the remote's default branch via origin/HEAD. Hard-coding `main`
# breaks repos that use `trunk`, `master`, etc.: the ancestry check
# silently no-ops (main_sha empty) and pass 3 falsely deletes unmerged
# work. Fall back to `main` only if origin/HEAD is unset, and warn.
default_branch=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null \
  | sed 's@^origin/@@' || true)
if [ -z "$default_branch" ]; then
  echo "warning: origin/HEAD not set; falling back to default branch 'main'" >&2
  default_branch=main
fi

# Shared deletion helper: drop any pinning worktree, then drop the branch.
# `force_flag` selects `-D` (squash-merge / merged-PR cases — branch tip is
# not git-ancestor of default, so `-d` would refuse) vs `-d` (gone-upstream
# pass, where ancestry was already proven and `-d` is the safer guard).
delete_branch_and_worktree() {
  local b="$1" force_flag="$2" reason="$3"
  for wt in $(git worktree list --porcelain | awk -v B="refs/heads/$b" '
    /^worktree / {p=$2} /^branch / && $2==B {print p}'); do
    case "$wt" in *.claude/worktrees/*)
      do_it "remove worktree $wt: git worktree remove --force '$wt'" ;;
    esac
  done
  do_it "delete branch $b${reason:+ ($reason)}: git branch $force_flag '$b'"
}

# Pass 1 — match merged PRs by headRefName (batch).
merged=$(gh pr list --state merged --limit 200 --json headRefName --jq '.[].headRefName' 2>/dev/null || true)

for b in $merged; do
  [ "$b" = "$default_branch" ] && continue
  [ "$b" = "$current" ] && continue
  git rev-parse --verify "refs/heads/$b" >/dev/null 2>&1 || continue
  delete_branch_and_worktree "$b" -D ""
done

# Pass 2 — per-branch PR-state probe. For every surviving local branch,
# ask gh whether a PR exists with that head. Catches squash-merge cases
# where pass 1's batch missed the branch (`--limit 200` truncation, or
# local branch name diverging from headRefName via an exact `head:` match
# below). `--include-closed` opts into deleting CLOSED-state branches.
#
# We probe a bounded set: only local branches that still exist. Each
# probe is one gh call — O(N_local_branches) net cost. Failures are
# soft (echo skip + continue) so a transient gh outage doesn't block
# pass 3.
for b in $(git for-each-ref --format='%(refname:short)' refs/heads/); do
  [ "$b" = "$default_branch" ] && continue
  [ "$b" = "$current" ] && continue
  state=$(gh pr list --search "head:$b" --state all --limit 1 \
    --json state --jq '.[0].state // ""' 2>/dev/null || true)
  case "$state" in
    MERGED)
      delete_branch_and_worktree "$b" -D "PR merged"
      ;;
    CLOSED)
      if [ "$include_closed" -eq 1 ]; then
        delete_branch_and_worktree "$b" -D "PR closed, --include-closed"
      fi
      ;;
  esac
done

# Pass 3 — branches whose upstream is gone (PR merged + remote branch pruned).
# `%(upstream:track)` emits "[gone]" when the upstream ref is unknown. We
# prune first so the marker reflects the remote's current state. The
# fetch must succeed by default: a silent failure (network, auth, proxy)
# leaves origin/<default> stale, which would let the ancestry check
# below approve deletion of work that landed locally but not on remote.
# `--allow-stale` opts into a degraded mode: warn and skip pass 3.
fetch_err=$(git fetch --prune 2>&1) && fetch_ok=1 || fetch_ok=0
if [ "$fetch_ok" -eq 0 ]; then
  if [ "$allow_stale" -eq 1 ]; then
    echo "warning: fetch failed; --allow-stale set, skipping gone-upstream pass" >&2
    echo "  detail: $fetch_err" >&2
    git worktree prune
    exit 0
  fi
  echo "error: fetch failed; refusing to run gone-upstream pass against stale refs" >&2
  echo "  detail: $fetch_err" >&2
  echo "  hint: re-run with --allow-stale to skip pass 3 and continue" >&2
  exit 1
fi

gone=$(git for-each-ref --format='%(refname:short) %(upstream:track)' refs/heads/ \
  | awk '$2=="[gone]" {print $1}')

default_sha=$(git rev-parse --verify "origin/$default_branch" 2>/dev/null \
  || git rev-parse --verify "$default_branch" 2>/dev/null || true)

for b in $gone; do
  [ "$b" = "$default_branch" ] && continue
  [ "$b" = "$current" ] && continue

  # Only delete branches whose tip is reachable from the default branch
  # — guards against an upstream that was deleted *before* the PR landed
  # (e.g. PR closed without merge, force-push to a different ref,
  # manual remote cleanup).
  if [ -n "$default_sha" ] && ! git merge-base --is-ancestor "refs/heads/$b" "$default_sha" 2>/dev/null; then
    echo "skip $b (upstream gone but not merged into $default_branch; preserve local work)"
    continue
  fi

  # -d (not -D) refuses unmerged branches as a final guard.
  delete_branch_and_worktree "$b" -d "upstream gone"
done

git worktree prune

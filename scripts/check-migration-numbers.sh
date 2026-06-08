#!/usr/bin/env bash
# check-migration-numbers.sh — mechanical gate against parallel-dispatch
# migration-number collision in internal/orchestrator/state/migrations/.
#
# Failure modes:
#   1. Duplicate version: two files share the same NNNN prefix integer.
#   2. Non-contiguous tail: Nmax != count(files) + count(KNOWN_GAPS).
#   3. PR multi-add: >1 new NNNN_*.sql added vs PR_BASE without a
#      `<!-- migration-multi-add-justified: <reason> -->` marker in PR body.
#
# Env overrides (also used by the fixture test):
#   MIGRATIONS_DIR — dir to scan (default: internal/orchestrator/state/migrations).
#   PR_BASE        — git ref for diff (default: origin/main).
#   KNOWN_GAPS     — space-separated bare integers (default: "8").
#   DIFF_ADDED     — test seam; newline-separated added paths. When set the
#                    script uses this instead of running `git diff`.
#   PR_BODY        — test seam; PR body text. When set the script uses this
#                    instead of `gh pr view "$GH_PR"`.
#   GH_PR          — PR number for `gh pr view` body lookup in CI.
#
# Portable to bash 3.2 (macOS default): no `mapfile`, no `declare -A`.
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${MIGRATIONS_DIR:=$REPO_ROOT/internal/orchestrator/state/migrations}"
: "${PR_BASE:=origin/main}"
# KNOWN_GAPS: space-separated integers. Default is "8" (the historical
# 0008_* skip). Use `KNOWN_GAPS=""` (set + empty) to declare no gaps —
# `:=` only fires when unset, so explicit empty stays empty.
if [ -z "${KNOWN_GAPS+x}" ]; then
  KNOWN_GAPS="8"
fi

if [ ! -d "$MIGRATIONS_DIR" ]; then
  echo "check-migration-numbers: MIGRATIONS_DIR not found: $MIGRATIONS_DIR" >&2
  exit 1
fi

# 1. Collect declared versions from filenames. Duplicate-detection uses a
# sorted-version list with adjacent-pair comparison (portable substitute for
# `declare -A`).
versions=""
basenames=""
count=0
while IFS= read -r f; do
  [ -z "$f" ] && continue
  base=$(basename "$f")
  v=$((10#${base:0:4}))
  versions="$versions$v
"
  basenames="$basenames$v $base
"
  count=$((count + 1))
done < <(find "$MIGRATIONS_DIR" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9]_*.sql' | sort)

if [ "$count" -eq 0 ]; then
  echo "check-migration-numbers: no migrations in $MIGRATIONS_DIR (skipping tail check)"
  exit 0
fi

# Duplicate detection: sort numeric, find uniq -d.
dup=$(printf '%s' "$versions" | grep -v '^$' | sort -n | uniq -d | head -1)
if [ -n "$dup" ]; then
  dup_files=$(printf '%s' "$basenames" | awk -v v="$dup" '$1 == v {print $2}' | paste -sd, -)
  echo "ERROR: duplicate version $dup: $dup_files" >&2
  exit 1
fi

# 2. Tail-contiguity check. Nmax = highest version; expected = count + |gaps|.
# shellcheck disable=SC2086
set -- $KNOWN_GAPS
gap_count=$#
nmax=$(printf '%s' "$versions" | grep -v '^$' | sort -n | tail -1)
expected=$(( count + gap_count ))
if [ "$nmax" -ne "$expected" ]; then
  echo "ERROR: non-contiguous tail — Nmax=$nmax expected=$expected (count=$count + known_gaps=$gap_count [${KNOWN_GAPS}])" >&2
  exit 1
fi

# 3. PR-diff multi-add check.
added_list=""
if [ -n "${DIFF_ADDED+x}" ]; then
  added_list="$DIFF_ADDED"
elif git rev-parse --verify "$PR_BASE" >/dev/null 2>&1; then
  added_list=$(git diff --name-only --diff-filter=A "$PR_BASE"...HEAD -- "$MIGRATIONS_DIR" 2>/dev/null | grep -E '/[0-9]{4}_[^/]+\.sql$' || true)
fi

if [ -n "$added_list" ]; then
  added_count=$(printf '%s\n' "$added_list" | grep -cE '[0-9]{4}_[^/]*\.sql$' || true)
  if [ "$added_count" -gt 1 ]; then
    pr_body=""
    if [ -n "${PR_BODY+x}" ]; then
      pr_body="$PR_BODY"
    elif [ -n "${GH_PR:-}" ] && command -v gh >/dev/null 2>&1; then
      pr_body=$(gh pr view "$GH_PR" --json body --jq .body 2>/dev/null || true)
    fi
    if printf '%s' "$pr_body" | grep -q 'migration-multi-add-justified:'; then
      :
    else
      added_csv=$(printf '%s' "$added_list" | tr '\n' ' ')
      echo "ERROR: PR adds $added_count migrations: $added_csv" >&2
      echo "hint: split into separate PRs OR add <!-- migration-multi-add-justified: <reason> --> to PR body" >&2
      exit 1
    fi
  fi
fi

echo "check-migration-numbers: ok — $count migrations, Nmax=$nmax (known_gaps=[${KNOWN_GAPS}])"
exit 0

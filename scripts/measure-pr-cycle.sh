#!/usr/bin/env bash
# measure-pr-cycle: per-stage PR cycle timing across recent merged PRs.
#
# Pulls the last N merged PRs and computes five lag stages:
#   1. createdAt          -> first push           (author draft lag)
#   2. first push         -> ci start             (workflow queue)
#   3. ci start           -> ci complete          (CI wall)
#   4. ci complete        -> reviewer-verdict     (review lag)
#   5. reviewer-verdict   -> mergedAt             (merge lag)
#
# Stage 4/5 source the Reviewer-recommendation token from the PR body;
# the body has no timestamp, so the reviewer-verdict timestamp is
# approximated as the latest of {last review submittedAt, last comment
# createdAt}. When neither is present we fall back to ci-complete (lag
# = 0) so the row still aggregates without skewing the merge-lag stage.
#
# Output: per-PR row + median/p95 aggregate per stage. One-shot; not a
# CI gate. Read-only against the live GitHub API via `gh`.
#
# Usage:
#   bash scripts/measure-pr-cycle.sh                 # last 20 merged PRs
#   bash scripts/measure-pr-cycle.sh --last 50       # last 50
#   bash scripts/measure-pr-cycle.sh --help

set -euo pipefail

LAST=20

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)
      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    --last)
      shift
      LAST="${1:-20}"
      shift ;;
    --last=*)
      LAST="${1#--last=}"
      shift ;;
    *)
      echo "measure-pr-cycle: unknown flag: $1" >&2
      exit 2 ;;
  esac
done

for bin in gh jq awk sort date; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "measure-pr-cycle: missing dependency: $bin" >&2
    exit 3
  fi
done

# Portable epoch parser: GNU date understands -d, BSD/macOS date wants -j -f.
to_epoch() {
  local ts="$1"
  if [ -z "$ts" ] || [ "$ts" = "null" ]; then
    echo ""
    return 0
  fi
  if date -d "$ts" +%s >/dev/null 2>&1; then
    date -d "$ts" +%s
  else
    date -j -u -f "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null || echo ""
  fi
}

# Render seconds as "Xm" or "Xh Ym" depending on magnitude. Negative or
# empty -> "-". Lag fields are integer minutes so the table aligns.
fmt_lag() {
  local s="${1:-}"
  if [ -z "$s" ] || [ "$s" = "null" ]; then
    echo "-"
    return 0
  fi
  if [ "$s" -lt 0 ] 2>/dev/null; then
    echo "-"
    return 0
  fi
  local m=$((s / 60))
  echo "${m}m"
}

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

# Step 1 - list merged PRs. Explicit --json allowlist per feedback_gh_minimal_fields.
prs_json=$(gh pr list --state merged -L "$LAST" \
  --json number,createdAt,mergedAt,title,body 2>/dev/null || echo "[]")

pr_count=$(echo "$prs_json" | jq 'length')
if [ "$pr_count" -eq 0 ]; then
  echo "measure-pr-cycle: no merged PRs found in window (--last $LAST)."
  exit 0
fi

# Step 2 - per-PR, fetch commits + headRefOid + reviews + comments via one
# `gh pr view`, then check-runs for the head sha via `gh api`. Per-PR cost:
# 2 gh calls. Cap N at the operator-supplied --last.
#
# Lag rows accumulate as TSV: pr<TAB>created<TAB>push<TAB>queue<TAB>ci<TAB>review<TAB>merge
tmp_rows=$(mktemp -t measure-pr-rows.XXXXXX)
trap 'rm -f "$tmp_rows"' EXIT

numbers=$(echo "$prs_json" | jq -r '.[].number')

while IFS= read -r n; do
  [ -z "$n" ] && continue
  pv=$(gh pr view "$n" \
    --json number,createdAt,mergedAt,title,body,commits,reviews,comments,headRefOid \
    2>/dev/null || echo "{}")

  created=$(echo "$pv" | jq -r '.createdAt // empty')
  merged=$(echo "$pv"  | jq -r '.mergedAt // empty')
  head_sha=$(echo "$pv" | jq -r '.headRefOid // empty')

  # First push proxy = earliest commit committedDate on the PR. PRs that
  # never amended after open have created == first-push; that's a true
  # zero, not a missing value.
  first_push=$(echo "$pv" \
    | jq -r '[.commits[]?.committedDate] | sort | .[0] // empty')

  # CI start/complete = min(started_at) / max(completed_at) across all
  # non-skipped check runs on the head SHA.
  if [ -n "$head_sha" ]; then
    cr=$(gh api "repos/{owner}/{repo}/commits/$head_sha/check-runs?per_page=100" \
      2>/dev/null || echo "{}")
  else
    cr="{}"
  fi
  ci_start=$(echo "$cr" \
    | jq -r '[.check_runs[]? | select(.conclusion != "skipped") | .started_at] | sort | .[0] // empty')
  ci_end=$(echo "$cr" \
    | jq -r '[.check_runs[]? | select(.conclusion != "skipped") | .completed_at] | sort | .[-1] // empty')

  # Reviewer-verdict timestamp = max(last review submittedAt, last
  # comment createdAt). Use only entries that mention the verdict token
  # OR (fallback) the latest of either when no token comment exists,
  # since the canonical APPROVE often lives in the PR body which has no
  # timestamp.
  reviewer_ts=$(echo "$pv" | jq -r '
    [
      ( .reviews[]?  | select(.submittedAt != null) | .submittedAt ),
      ( .comments[]? | select(.createdAt   != null) | .createdAt   )
    ] | sort | .[-1] // empty')

  e_created=$(to_epoch "$created")
  e_merged=$(to_epoch  "$merged")
  e_push=$(to_epoch    "$first_push")
  e_ci_s=$(to_epoch    "$ci_start")
  e_ci_e=$(to_epoch    "$ci_end")
  e_rev=$(to_epoch     "$reviewer_ts")

  # Fallback chain: if reviewer_ts missing, treat ci_end as the verdict
  # boundary so merge-lag stays meaningful and review-lag is reported "-".
  if [ -z "$e_rev" ]; then e_rev="$e_ci_e"; fi

  push_lag=""
  queue_lag=""
  ci_wall=""
  review_lag=""
  merge_lag=""

  if [ -n "$e_created" ] && [ -n "$e_push" ]; then
    push_lag=$((e_push - e_created))
  fi
  if [ -n "$e_push" ] && [ -n "$e_ci_s" ]; then
    queue_lag=$((e_ci_s - e_push))
  fi
  if [ -n "$e_ci_s" ] && [ -n "$e_ci_e" ]; then
    ci_wall=$((e_ci_e - e_ci_s))
  fi
  if [ -n "$e_ci_e" ] && [ -n "$e_rev" ]; then
    review_lag=$((e_rev - e_ci_e))
  fi
  if [ -n "$e_rev" ] && [ -n "$e_merged" ]; then
    merge_lag=$((e_merged - e_rev))
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$n" "${created:0:10}" \
    "${push_lag:-}" "${queue_lag:-}" "${ci_wall:-}" "${review_lag:-}" "${merge_lag:-}" \
    >> "$tmp_rows"
done <<< "$numbers"

# Step 3 - render table.
printf '\n%-6s  %-10s  %8s  %8s  %8s  %8s  %8s\n' \
  "PR" "CREATED" "PUSH" "QUEUE" "CI" "REVIEW" "MERGE"
printf '%-6s  %-10s  %8s  %8s  %8s  %8s  %8s\n' \
  "----" "----------" "--------" "--------" "--------" "--------" "--------"

while IFS=$'\t' read -r pr created push queue ci review merge; do
  printf '%-6s  %-10s  %8s  %8s  %8s  %8s  %8s\n' \
    "$pr" "$created" \
    "$(fmt_lag "$push")" "$(fmt_lag "$queue")" "$(fmt_lag "$ci")" \
    "$(fmt_lag "$review")" "$(fmt_lag "$merge")"
done < "$tmp_rows"

# Step 4 - per-stage aggregates. Median + p95 over non-empty seconds.
echo
agg() {
  local label="$1" col="$2"
  local values
  values=$(awk -F'\t' -v c="$col" 'NF >= c && $c != "" && $c ~ /^-?[0-9]+$/ && $c >= 0 {print $c}' "$tmp_rows" | sort -n)
  local count
  count=$(printf '%s\n' "$values" | grep -c . || true)
  if [ "$count" -eq 0 ]; then
    printf '%-8s  median=%s  p95=%s  n=0\n' "$label" "-" "-"
    return
  fi
  local median p95
  median=$(printf '%s\n' "$values" | awk -v n="$count" 'NR == int((n+1)/2) {print; exit}')
  # p95 = ceil(0.95 * n)-th value, 1-indexed; clamp to last when n small.
  local p95_idx
  p95_idx=$(awk -v n="$count" 'BEGIN{ v=n*0.95; i=int(v); if (v > i) i++; if (i < 1) i=1; if (i > n) i=n; print i }')
  p95=$(printf '%s\n' "$values" | awk -v i="$p95_idx" 'NR == i {print; exit}')
  printf '%-8s  median=%-6s  p95=%-6s  n=%s\n' \
    "$label" "$(fmt_lag "$median")" "$(fmt_lag "$p95")" "$count"
}

echo "aggregates (median / p95 per stage; - = no data)"
agg "push"   3
agg "queue"  4
agg "ci"     5
agg "review" 6
agg "merge"  7

echo
echo "measure-pr-cycle: scanned $pr_count merged PRs."

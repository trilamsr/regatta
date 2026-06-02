#!/usr/bin/env bash
# agent-status: summarize current autonomous-session state for the operator.
#
# Four panels:
#   1. Active local worktrees under .claude/worktrees/agent-*/ (branch + last commit).
#   2. Open PRs (number, title, automerge, CI rollup).
#   3. Recent merges in the last $RECENT_HOURS hours (default 4).
#   4. Open issue count + breakdown by label.
#
# Read-only. Issues no `git` or `gh` writes. Skips network panels under
# --no-network so the smoke test runs offline.
#
# Usage:
#   bash scripts/agent-status.sh                # full report
#   bash scripts/agent-status.sh --no-network   # skip gh calls (worktrees only)
#   RECENT_HOURS=8 bash scripts/agent-status.sh # widen recent-merges window

set -euo pipefail

no_network=0
for arg in "$@"; do
  case "$arg" in
    --no-network) no_network=1 ;;
    -h|--help)
      sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "agent-status: unknown flag: $arg" >&2; exit 2 ;;
  esac
done

recent_hours="${RECENT_HOURS:-4}"

# Resolve repo root so the script works from anywhere.
repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

hr() { printf -- '----------------------------------------------------------------------\n'; }
section() { printf '\n== %s ==\n' "$1"; }

# Panel 1 — active agent worktrees.
section "agent worktrees"
# Iterate `git worktree list --porcelain` records and keep only those whose
# path contains .claude/worktrees/agent-. For each, print branch + last
# commit short-sha + last commit relative time.
git worktree list --porcelain | awk '
  /^worktree / {p=$2}
  /^HEAD / {h=$2}
  /^branch / {b=$2}
  /^$/ {
    if (p ~ /\.claude\/worktrees\/agent-/) {
      print p "\t" b "\t" h
    }
    p=""; h=""; b=""
  }
  END {
    if (p ~ /\.claude\/worktrees\/agent-/) {
      print p "\t" b "\t" h
    }
  }
' | sort | while IFS=$'\t' read -r wpath branch head; do
  # `git log -1` against the worktree's HEAD, formatted with ISO-strict
  # date + relative.
  ts=$(git log -1 --format='%cI %cr' "$head" 2>/dev/null || echo "?")
  short=$(printf '%s' "$head" | cut -c1-8)
  printf '  %s  %s  %s  (%s)\n' "$(basename "$wpath")" "${branch#refs/heads/}" "$short" "$ts"
done

# Bail out before any network panels if --no-network.
if [ "$no_network" -eq 1 ]; then
  section "skipping PR / issue panels (--no-network)"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo
  echo "agent-status: gh not installed; skipping PR + issue panels" >&2
  exit 0
fi

# Panel 2 — open PRs.
section "open PRs"
prs_json=$(gh pr list --state open --limit 100 \
  --json number,title,headRefName,autoMergeRequest,statusCheckRollup \
  2>/dev/null || echo "[]")
echo "$prs_json" | jq -r '
  if length == 0 then "  (none)" else
    sort_by(.number) | .[] |
    # CI rollup: count FAILURE / PENDING / SUCCESS across all checks.
    (.statusCheckRollup // []) as $cs |
    ($cs | map(select(.conclusion == "FAILURE")) | length) as $fail |
    ($cs | map(select(.status != "COMPLETED")) | length) as $pend |
    ($cs | map(select(.conclusion == "SUCCESS")) | length) as $ok |
    (if $fail > 0 then "FAIL(\($fail))"
     elif $pend > 0 then "PEND(\($pend))"
     elif $ok > 0 then "OK(\($ok))"
     else "no-checks" end) as $ci |
    (if .autoMergeRequest != null then "auto" else "-" end) as $am |
    "  #\(.number)  \($am)  \($ci)  \(.title)"
  end
'

# Panel 3 — recent merges.
section "merges in last ${recent_hours}h"
# `gh pr list --search "merged:>=DATE"` uses ISO-8601 dates. Build an
# UTC timestamp `recent_hours` ago. macOS BSD date and GNU date disagree
# on flags, so probe.
if date -u -v-1H +%Y-%m-%dT%H:%M:%SZ >/dev/null 2>&1; then
  since=$(date -u -v-"${recent_hours}"H +%Y-%m-%dT%H:%M:%SZ)
else
  since=$(date -u -d "${recent_hours} hours ago" +%Y-%m-%dT%H:%M:%SZ)
fi
merged_json=$(gh pr list --state merged --limit 100 \
  --search "merged:>=${since}" \
  --json number,title,mergedAt \
  2>/dev/null || echo "[]")
echo "$merged_json" | jq -r --arg since "$since" '
  if length == 0 then "  (none since \($since))" else
    sort_by(.mergedAt) | reverse | .[] |
    "  #\(.number)  \(.mergedAt)  \(.title)"
  end
'

# Panel 4 — open issues + label breakdown.
section "open issues"
issues_json=$(gh issue list --state open --limit 500 \
  --json number,labels \
  2>/dev/null || echo "[]")
total=$(echo "$issues_json" | jq 'length')
printf '  total: %s\n' "$total"
if [ "$total" -gt 0 ]; then
  printf '  by label:\n'
  echo "$issues_json" | jq -r '
    [ .[] | (.labels // []) | (if length == 0 then [{name:"(unlabeled)"}] else . end) | .[] | .name ]
    | group_by(.) | map({label: .[0], n: length})
    | sort_by(-.n) | .[]
    | "    \(.n)\t\(.label)"
  '
fi

echo
hr

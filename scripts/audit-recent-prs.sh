#!/usr/bin/env bash
# audit-recent-prs.sh — post-merge subagent-quality audit (closes #1033).
#
# Daily cron scans PR bodies merged in the last 24h and flags:
#   - Malformed `Reviewer-recommendation:` tokens — value must be EXACTLY
#     APPROVE | REVISE | BLOCK on its own line. Lowercase, extra prose,
#     hedge text ("approve with caveats", "APPROVE -- ship it") flagged.
#   - Phantom-dep cites — `#NNN` refs that don't resolve to a real PR or
#     issue in the repo. Requires --known-refs-file (newline ints); skip
#     silently when absent so the malformed-token check still runs.
#   - Missing token — only when --require-token is passed (default off
#     because [CHORE]/[DOCS] PRs legitimately skip review).
#
# On hit the script prints findings to stdout/stderr and exits non-zero so
# the CI workflow can file a tracking issue with the captured output. In
# `--gh-mode` the script discovers merged PRs via `gh pr list --search
# 'merged:>=<ISO>'` and audits each body — but the heavy lifting is the
# per-body audit, exposed via --audit-body-file for unit tests.
#
# Inputs:
#   --audit-body-file <path>  audit a single PR body (test fixture path).
#                             MUST be paired with --pr <number> for
#                             reporting context.
#   --pr <number>             PR number for finding labels.
#   --known-refs-file <path>  newline-delimited list of valid PR/issue
#                             numbers. Phantom check active only when set.
#   --require-token           flag bodies missing the Reviewer-recommendation
#                             line entirely (off by default; CHORE/DOCS skip).
#   --gh-mode                 scan merged PRs in the last --since window via
#                             `gh pr list`; reports each body. Mutually
#                             exclusive with --audit-body-file.
#   --since <ISO8601>         start of the window for --gh-mode. Default:
#                             24h ago (date -u -v-24H if BSD; date -u -d).
#
# Exit:
#   0 no findings.
#   1 one or more findings emitted.
#   3 usage error.
#
# Closes #1033.

set -uo pipefail

BODY_FILE=""
PR_NUM=""
REFS_FILE=""
REQUIRE_TOKEN=0
GH_MODE=0
SINCE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --audit-body-file) BODY_FILE="$2"; shift 2 ;;
    --pr) PR_NUM="$2"; shift 2 ;;
    --known-refs-file) REFS_FILE="$2"; shift 2 ;;
    --require-token) REQUIRE_TOKEN=1; shift ;;
    --gh-mode) GH_MODE=1; shift ;;
    --since) SINCE="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "audit-recent-prs: unknown flag: $1" >&2
      exit 3
      ;;
  esac
done

if [ "$GH_MODE" -eq 1 ] && [ -n "$BODY_FILE" ]; then
  echo "audit-recent-prs: --gh-mode and --audit-body-file are mutually exclusive" >&2
  exit 3
fi

# Per-body audit. Writes findings to stdout (1 per line), prefixed with the
# PR number when set. Returns the count of findings via the global FINDINGS.
audit_body() {
  local body_path="$1"
  local pr_label="$2"
  local refs_path="$3"
  local require_token="$4"
  local findings=0

  if [ ! -f "$body_path" ]; then
    echo "audit-recent-prs: body file not found: $body_path" >&2
    return 1
  fi

  # Strip fenced code blocks (mirrors check-reviewer-verdict.sh #922 logic).
  # Stale/example tokens inside ``` should not trip the audit.
  local unfenced
  unfenced=$(awk '
    /^```/ { in_fence = !in_fence; next }
    !in_fence { print }
  ' "$body_path")

  # Malformed token check. Strict shape: optional leading whitespace, then
  # "Reviewer-recommendation:" (case-insensitive header), then exactly one
  # of APPROVE | REVISE | BLOCK (uppercase), then optional trailing
  # whitespace. ANY deviation flagged.
  local token_lines
  token_lines=$(printf '%s\n' "$unfenced" | grep -iE '^[[:space:]]*Reviewer-recommendation:' || true)

  if [ -z "$token_lines" ]; then
    if [ "$require_token" -eq 1 ]; then
      echo "PR ${pr_label}: missing Reviewer-recommendation token"
      findings=$((findings + 1))
    fi
  else
    while IFS= read -r line; do
      [ -z "$line" ] && continue
      # Reject when not exact APPROVE/REVISE/BLOCK on the value side.
      if ! printf '%s' "$line" | grep -qE '^[[:space:]]*[Rr]eviewer-[Rr]ecommendation:[[:space:]]+(APPROVE|REVISE|BLOCK)[[:space:]]*$'; then
        echo "PR ${pr_label}: malformed Reviewer-recommendation token: ${line}"
        findings=$((findings + 1))
      fi
    done <<< "$token_lines"
  fi

  # Phantom-dep cites. Extract every #NNN ref from the unfenced body and
  # check membership against --known-refs-file. Skip silently when refs
  # file absent — auditor cannot decide without an authoritative list.
  if [ -n "$refs_path" ] && [ -f "$refs_path" ]; then
    local refs_used
    refs_used=$(printf '%s\n' "$unfenced" | grep -oE '#[0-9]+' | sort -u || true)
    if [ -n "$refs_used" ]; then
      while IFS= read -r ref; do
        [ -z "$ref" ] && continue
        local num="${ref#\#}"
        if ! grep -qx "$num" "$refs_path"; then
          echo "PR ${pr_label}: phantom dep cite ${ref} (not found in known-refs)"
          findings=$((findings + 1))
        fi
      done <<< "$refs_used"
    fi
  fi

  return "$findings"
}

if [ -n "$BODY_FILE" ]; then
  if [ -z "$PR_NUM" ]; then
    echo "audit-recent-prs: --audit-body-file requires --pr" >&2
    exit 3
  fi
  audit_body "$BODY_FILE" "$PR_NUM" "$REFS_FILE" "$REQUIRE_TOKEN"
  rc=$?
  if [ "$rc" -gt 0 ]; then
    exit 1
  fi
  exit 0
fi

if [ "$GH_MODE" -eq 1 ]; then
  if [ -z "$SINCE" ]; then
    # 24h ago, ISO8601, UTC. macOS BSD `date -v` differs from GNU `-d`; try both.
    SINCE=$(date -u -v-24H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
      || date -u -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ)
  fi
  if ! command -v gh >/dev/null 2>&1; then
    echo "audit-recent-prs: --gh-mode requires the gh CLI" >&2
    exit 3
  fi

  # Build the known-refs file from the repo's recent issue/PR numbers when
  # --known-refs-file is absent. Covers the universe of valid refs without
  # forcing the caller to maintain a list.
  if [ -z "$REFS_FILE" ]; then
    REFS_FILE=$(mktemp)
    trap 'rm -f "$REFS_FILE"' EXIT
    gh issue list --state all --limit 2000 --json number --jq '.[].number' > "$REFS_FILE" 2>/dev/null || true
    gh pr list --state all --limit 2000 --json number --jq '.[].number' >> "$REFS_FILE" 2>/dev/null || true
  fi

  total_findings=0
  prs=$(gh pr list --state merged --search "merged:>=${SINCE}" \
    --limit 50 --json number --jq '.[].number' 2>/dev/null || true)
  if [ -z "$prs" ]; then
    echo "audit-recent-prs: no merged PRs since ${SINCE}"
    exit 0
  fi
  for pr in $prs; do
    body_tmp=$(mktemp)
    gh pr view "$pr" --json body --jq '.body' > "$body_tmp" 2>/dev/null || true
    audit_body "$body_tmp" "$pr" "$REFS_FILE" "$REQUIRE_TOKEN"
    rc=$?
    total_findings=$((total_findings + rc))
    rm -f "$body_tmp"
  done
  if [ "$total_findings" -gt 0 ]; then
    echo "audit-recent-prs: $total_findings findings across merged PRs since ${SINCE}" >&2
    exit 1
  fi
  exit 0
fi

echo "audit-recent-prs: pass --audit-body-file or --gh-mode" >&2
exit 3

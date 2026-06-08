#!/usr/bin/env bash
# check-reviewer-verdict.sh - fail when a load-bearing PR body does not
# carry a `Reviewer-recommendation: APPROVE` token.
#
# Drift pattern this closes: per session retro 2026-06-04, 3 CRITICAL +
# 3 HIGH defects (#848 rate-limit, #859 shell injection, #863 wire,
# #837 prompt injection, #886 L4 race, #888 scheduler poll race) were
# caught by reviewer subagents POST-PR-open, then filed as tracking
# issues. CLAUDE.md "TDD + review" mandates reviewer on every
# load-bearing PR but enforcement is operator-discretion.
#
# Gate:
#   Inputs:
#     --pr <number>       fetch body via `gh pr view`
#     --body-file <path>  read body from a local file (CI + fixtures)
#     --load-bearing      treat the PR as load-bearing (CI passes when
#                         the changed-paths heuristic matches)
#     --changed-paths-file <path>  newline-delimited file of changed paths.
#                         Any path matching the agent-rule / CI-gate
#                         load-bearing list (CLAUDE.md, Makefile,
#                         Makefile.d/*, .github/workflows/*,
#                         docs/engineer/dispatch-templates/*,
#                         scripts/check-*.sh) sets load-bearing=1 AND
#                         BYPASSES the release-notes category auto-skip
#                         (closes #985 #986 retro audit 2026-06-08).
#     --skip              short-circuit pass (operator-discretion escape)
#
#   Auto-skip: release-notes prefix in [CHORE]/[DOCS]/[CI]/[NONE]/[CHANGE]
#   (matches scripts/check-scorecard.sh category-exempt list). NOT applied
#   when --changed-paths-file flags a load-bearing surface above — those
#   surfaces are themselves the category being reviewed.
#
#   Pass: body contains `Reviewer-recommendation: APPROVE` ON ITS OWN
#   line (case-insensitive, leading/trailing whitespace allowed). Tokens
#   inside ```-fenced code blocks are stripped before the scan so stale
#   examples or draft snippets cannot beat the bare footer token (#922).
#
#   Fail: token absent OR equals REVISE/BLOCK when --load-bearing.
#
# Exit:
#   0 pass / category-exempt / not load-bearing.
#   1 missing token on load-bearing PR.
#   2 REVISE / BLOCK recommendation.
#   3 usage error.
#
# Closes #899.

set -uo pipefail

PR_NUM=""
BODY_FILE=""
LOAD_BEARING=0
PATHS_FILE=""
SKIP=0

while [ $# -gt 0 ]; do
  case "$1" in
    --pr) PR_NUM="$2"; shift 2 ;;
    --body-file) BODY_FILE="$2"; shift 2 ;;
    --load-bearing) LOAD_BEARING=1; shift ;;
    --changed-paths-file) PATHS_FILE="$2"; shift 2 ;;
    --skip) SKIP=1; shift ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "check-reviewer-verdict: unknown flag: $1" >&2
      exit 3
      ;;
  esac
done

if [ "$SKIP" -eq 1 ]; then
  exit 0
fi

if [ -n "$PR_NUM" ] && [ -n "$BODY_FILE" ]; then
  echo "check-reviewer-verdict: --pr and --body-file are mutually exclusive" >&2
  exit 3
fi

if [ -n "$PR_NUM" ]; then
  BODY_FILE=$(mktemp)
  trap 'rm -f "$BODY_FILE"' EXIT
  if ! gh pr view "$PR_NUM" --json body --jq '.body' > "$BODY_FILE"; then
    echo "check-reviewer-verdict: gh pr view $PR_NUM failed" >&2
    exit 3
  fi
fi

if [ -z "$BODY_FILE" ] || [ ! -f "$BODY_FILE" ]; then
  echo "check-reviewer-verdict: no body source (use --pr or --body-file)" >&2
  exit 3
fi

# Path classifier (closes #985 #986). When --changed-paths-file lists any
# agent-rule or CI-gate surface, flag load-bearing AND bypass category
# auto-skip — refactors to these surfaces self-tagged [CHORE]/[DOCS] in
# the 2026-06-08 retro and slipped past review.
LOAD_BEARING_BY_PATH=0
if [ -n "$PATHS_FILE" ]; then
  if [ ! -f "$PATHS_FILE" ]; then
    echo "check-reviewer-verdict: --changed-paths-file $PATHS_FILE not found" >&2
    exit 3
  fi
  while IFS= read -r changed_path; do
    [ -z "$changed_path" ] && continue
    case "$changed_path" in
      CLAUDE.md|Makefile|Makefile.d/*|.github/workflows/*|docs/engineer/dispatch-templates/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      scripts/check-*.sh)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
    esac
  done < "$PATHS_FILE"
  if [ "$LOAD_BEARING_BY_PATH" -eq 1 ]; then
    LOAD_BEARING=1
  fi
fi

CATEGORY=$(awk '
  /^```release-notes/ { in_block = 1; next }
  in_block && /^```/ { exit }
  in_block { print; exit }
' "$BODY_FILE" | grep -oE '^\[[A-Z]+\]' | head -1)

if [ "$LOAD_BEARING_BY_PATH" -ne 1 ]; then
  case "$CATEGORY" in
    '[CHORE]'|'[DOCS]'|'[CI]'|'[NONE]'|'[CHANGE]')
      exit 0
      ;;
  esac
fi

if [ "$LOAD_BEARING" -ne 1 ]; then
  exit 0
fi

# Strip ```-fenced blocks so stale draft tokens cannot shadow the bare
# footer token (#922). Mirrors the category-extract awk above; toggle
# state-machine flips on every ``` line. Pick the LAST bare token so a
# stale REVISE preceding a fresh APPROVE on body re-edit does not win
# (#923).
RECOMMENDATION=$(awk '
  /^```/ { in_fence = !in_fence; next }
  !in_fence { print }
' "$BODY_FILE" \
  | grep -iE '^[[:space:]]*Reviewer-recommendation:' \
  | tail -1 \
  | sed -E 's/^[[:space:]]*Reviewer-recommendation:[[:space:]]*//I' \
  | tr -d '[:space:]' \
  | tr '[:lower:]' '[:upper:]')

case "$RECOMMENDATION" in
  APPROVE)
    exit 0
    ;;
  REVISE|BLOCK)
    echo "check-reviewer-verdict: Reviewer-recommendation is $RECOMMENDATION on a load-bearing PR." >&2
    echo "  Fix: address the findings, then update the body to Reviewer-recommendation: APPROVE." >&2
    exit 2
    ;;
  '')
    echo "check-reviewer-verdict: load-bearing PR is missing Reviewer-recommendation token in body." >&2
    echo "  Fix: dispatch an independent reviewer subagent per CLAUDE.md 'TDD + review'." >&2
    echo "  Add to PR body footer (bare, NOT in a code block):" >&2
    echo "    Reviewer-agent-id: <id>" >&2
    echo "    Reviewer-recommendation: APPROVE|REVISE|BLOCK" >&2
    exit 1
    ;;
  *)
    echo "check-reviewer-verdict: unrecognized Reviewer-recommendation value: $RECOMMENDATION (expected APPROVE / REVISE / BLOCK)." >&2
    exit 1
    ;;
esac

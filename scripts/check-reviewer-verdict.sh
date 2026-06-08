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
#                         scripts/check-*.sh) OR the load-bearing-doc
#                         list (docs/engineer/specs/*.md,
#                         docs/engineer/briefs/*.md) sets load-bearing=1
#                         AND BYPASSES the release-notes category auto-skip
#                         (closes #985 #986 #991 retro audit 2026-06-08).
#     --skip              short-circuit pass (operator-discretion escape)
#     --pr-author <login> PR author login (e.g. from `gh api .../pulls/N
#                         --jq .user.login`). When provided AND the PR
#                         carries Reviewer-recommendation: APPROVE on a
#                         load-bearing surface, the script also enforces
#                         that a bare `Reviewer-agent-id:` line exists AND
#                         its value differs from <login>. Closes the
#                         self-tag loophole (`feedback_no_self_tagged_approve`):
#                         author writing own APPROVE token == zero
#                         adversarial pass.
#     --automerge-enabled signals that `autoMergeRequest != null` on the PR
#                         (queried via `gh pr view --json autoMergeRequest`).
#                         When the PR is also load-bearing AND
#                         `Reviewer-agent-id:` is present, the gate fails
#                         closed with stderr token
#                         `automerge_with_agent_id_on_load_bearing`. Closes
#                         #1046: agent both writes its own APPROVE and
#                         enables automerge, leaving no operator window for
#                         human merge per CLAUDE.md `gates::human_merge`.
#                         Operator can always disable automerge then merge
#                         manually.
#
#   Operator escape (rare): include `<!-- reviewer-skip-justified: <reason
#   ≥4 chars> -->` in the PR body to bypass the self-tag mismatch check
#   for trivial doc/typo/dep-bump cases. The token-present check still
#   runs — the escape only relaxes author ≠ Reviewer-agent-id.
#
#   Auto-skip: release-notes prefix in [CHORE]/[DOCS]/[CI]/[NONE]/[CHANGE]
#   (matches scripts/check-scorecard.sh category-exempt list). NOT applied
#   when --changed-paths-file flags a load-bearing surface above — those
#   surfaces are themselves the category being reviewed.
#
#   Load-bearing-doc carve-out: even when release-notes is [DOCS], the
#   gate refuses to auto-skip when the diff touches load-bearing prose
#   surfaces — docs/engineer/specs/*.md, docs/engineer/briefs/*.md,
#   docs/engineer/dispatch-templates/*.md, or CLAUDE.md. Operator finding
#   2026-06-08: design/spec PRs landed w/ self-included adversarial
#   sections (not independent review). Per `feedback_adversarial_review_every_step`.
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
PR_AUTHOR=""
AUTOMERGE_ENABLED=0

while [ $# -gt 0 ]; do
  case "$1" in
    --pr) PR_NUM="$2"; shift 2 ;;
    --body-file) BODY_FILE="$2"; shift 2 ;;
    --load-bearing) LOAD_BEARING=1; shift ;;
    --changed-paths-file) PATHS_FILE="$2"; shift 2 ;;
    --skip) SKIP=1; shift ;;
    --pr-author) PR_AUTHOR="$2"; shift 2 ;;
    --automerge-enabled) AUTOMERGE_ENABLED=1; shift ;;
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

# Path classifier (closes #985 #986 #991). When --changed-paths-file lists any
# agent-rule, CI-gate, or load-bearing-doc surface, flag load-bearing AND
# bypass category auto-skip — refactors to these surfaces self-tagged
# [CHORE]/[DOCS] in the 2026-06-08 retro and slipped past review.
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
      docs/engineer/specs/*.md|docs/engineer/briefs/*.md)
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

REVIEWER_AGENT_ID=$(awk '
  /^```/ { in_fence = !in_fence; next }
  !in_fence { print }
' "$BODY_FILE" \
  | grep -iE '^[[:space:]]*Reviewer-agent-id:' \
  | tail -1 \
  | sed -E 's/^[[:space:]]*Reviewer-agent-id:[[:space:]]*//I' \
  | tr -d '[:space:]')

case "$RECOMMENDATION" in
  APPROVE)
    # Automerge guard (closes #1046): if the agent both writes its own
    # APPROVE and enables automerge, no operator window exists for human
    # merge per CLAUDE.md `gates::human_merge`. Token-present check still
    # runs after the missing-agent-id branch below to keep error messages
    # specific; the automerge guard fires only when an agent-id is present.
    if [ "$AUTOMERGE_ENABLED" -eq 1 ] && [ -n "$REVIEWER_AGENT_ID" ]; then
      echo "check-reviewer-verdict: automerge_with_agent_id_on_load_bearing — autoMergeRequest is enabled on a load-bearing PR carrying Reviewer-agent-id: $REVIEWER_AGENT_ID." >&2
      echo "  Per CLAUDE.md gates::human_merge, agent-written APPROVE + agent-enabled automerge leaves no operator window for human merge." >&2
      echo "  Fix: disable automerge (gh pr merge --disable-auto <PR>), then operator merges manually after independent review." >&2
      exit 1
    fi
    # Token-present check (closes #999/#1001/#1002 self-tag bypass per
    # `feedback_no_self_tagged_approve`): a load-bearing APPROVE without
    # a named reviewer is a self-tagged approval — independent review
    # never happened.
    if [ -z "$REVIEWER_AGENT_ID" ]; then
      echo "check-reviewer-verdict: load-bearing PR has Reviewer-recommendation: APPROVE but is missing the Reviewer-agent-id token in body." >&2
      echo "  An APPROVE without a named reviewer is a self-tagged approval — independent review never happened." >&2
      echo "  Fix: dispatch an independent reviewer subagent per CLAUDE.md feedback_no_self_tagged_approve." >&2
      echo "  Add to PR body footer (bare, NOT in a code block):" >&2
      echo "    Reviewer-agent-id: <id>" >&2
      echo "    Reviewer-recommendation: APPROVE" >&2
      exit 1
    fi
    # Operator escape via `<!-- reviewer-skip-justified: <reason ≥4 chars> -->`
    # bypasses BOTH the allowlist check and the author-mismatch check
    # (rare, trivial doc/typo/dep-bump only). The token-present check
    # above always applies.
    JUSTIFICATION=$(grep -oE '<!--[[:space:]]*reviewer-skip-justified:[[:space:]]*[^>]*-->' "$BODY_FILE" \
      | head -1 \
      | sed -E 's/^<!--[[:space:]]*reviewer-skip-justified:[[:space:]]*//; s/[[:space:]]*-->$//' \
      | sed -E 's/[[:space:]]+$//')
    JUSTIFICATION_LEN=${#JUSTIFICATION}
    if [ "$JUSTIFICATION_LEN" -ge 4 ]; then
      exit 0
    fi
    # Independent-reviewer allowlist per `feedback_no_self_tagged_approve`.
    # Real reviewer agent IDs match one of two canonical shapes:
    #   (a) harness shape `^a[0-9a-f]{16}$` (17-char hex, e.g. `a6614259e2388c0ee`)
    #   (b) named-subagent shape `^(cavecrew|designer|triage|implementer|reviewer)-[a-z0-9-]+$`
    # Any other value is treated as a self-tag escape (#1036 used
    # `main-thread-adversarial-self`; #1037/#1038 used `self-tagged-defer`)
    # and rejected. Allowlist beats denylist: workers can invent new
    # escape strings, but cannot fake the canonical prefixes without
    # actually dispatching a real subagent.
    if ! echo "$REVIEWER_AGENT_ID" | grep -qE '^(a[0-9a-f]{16}|(cavecrew|designer|triage|implementer|reviewer)-[a-z0-9-]+)$'; then
      echo "check-reviewer-verdict: Reviewer-agent-id '$REVIEWER_AGENT_ID' does not match independent-reviewer allowlist on a load-bearing PR." >&2
      echo "  Allowed shapes: harness ID '^a[0-9a-f]{16}\$' (e.g. 'a6614259e2388c0ee') OR named subagent '^(cavecrew|designer|triage|implementer|reviewer)-<slug>\$'." >&2
      echo "  Self-tag escapes (e.g. 'main-thread-adversarial-self', 'self-tagged-defer') rejected per CLAUDE.md 'No self-tagged Reviewer-recommendation: APPROVE'." >&2
      echo "  Fix: dispatch independent reviewer subagent in fresh slot; paste its agent ID into the PR body footer." >&2
      echo "  Operator escape (rare, trivial doc/typo/dep-bump only):" >&2
      echo "    <!-- reviewer-skip-justified: <reason ≥4 chars> -->" >&2
      exit 1
    fi
    # Self-tag mismatch check: when caller passes --pr-author, the bare
    # `Reviewer-agent-id:` value MUST differ from the author login.
    if [ -n "$PR_AUTHOR" ] && [ "$REVIEWER_AGENT_ID" = "$PR_AUTHOR" ]; then
      echo "check-reviewer-verdict: self-tagged APPROVE rejected — Reviewer-agent-id ($REVIEWER_AGENT_ID) equals PR author ($PR_AUTHOR)." >&2
      echo "  Fix: dispatch an independent reviewer subagent in a fresh slot per CLAUDE.md 'TDD + review'." >&2
      echo "  Update PR body footer with the reviewer subagent id:" >&2
      echo "    Reviewer-agent-id: <independent-reviewer-id>  # MUST differ from $PR_AUTHOR" >&2
      echo "    Reviewer-recommendation: APPROVE" >&2
      echo "  Operator escape (rare, trivial doc/typo/dep-bump only):" >&2
      echo "    <!-- reviewer-skip-justified: <reason ≥4 chars> -->" >&2
      exit 1
    fi
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

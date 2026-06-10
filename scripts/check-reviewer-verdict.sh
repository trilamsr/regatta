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
#                         (closes #985 #986 retro audit).
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
#                         enables automerge, leaving zero operator window
#                         between APPROVE-token landing and merge. Operator
#                         can always disable automerge then merge manually.
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
#   examples or draft snippets cannot beat the bare footer token.
#
#   Fail: token absent OR equals REVISE/BLOCK/INSUFFICIENT_EVIDENCE when
#   --load-bearing. INSUFFICIENT_EVIDENCE additionally requires a bare
#   `Confidence-evidence-needed: #NNN` token in the body footer; missing
#   token → exit 1, present token → exit 2 (same as REVISE, prompts
#   resolution).
#
# Exit:
#   0 pass / category-exempt / not load-bearing.
#   1 missing token on load-bearing PR (or INSUFFICIENT_EVIDENCE missing Confidence-evidence-needed).
#   2 REVISE / BLOCK / INSUFFICIENT_EVIDENCE recommendation.
#   3 usage error.
#
#
# Refactor #1044: per-check functions live under scripts/lib/reviewer-verdict/;
# this top-level file is the orchestrator that sources them. Behavior is
# byte-equivalent to the pre-split implementation — see scripts/check-reviewer-verdict_test.sh.

set -uo pipefail

RV_LIB_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/lib/reviewer-verdict"
# shellcheck source=lib/reviewer-verdict/args.sh
. "$RV_LIB_DIR/args.sh"
# shellcheck source=lib/reviewer-verdict/body-source.sh
. "$RV_LIB_DIR/body-source.sh"
# shellcheck source=lib/reviewer-verdict/path-classifier.sh
. "$RV_LIB_DIR/path-classifier.sh"
# shellcheck source=lib/reviewer-verdict/category-skip.sh
. "$RV_LIB_DIR/category-skip.sh"
# shellcheck source=lib/reviewer-verdict/token-extract.sh
. "$RV_LIB_DIR/token-extract.sh"
# shellcheck source=lib/reviewer-verdict/verdict.sh
. "$RV_LIB_DIR/verdict.sh"

rv_parse_args "$@"

if [ "$SKIP" -eq 1 ]; then
  exit 0
fi

rv_resolve_body
rv_classify_paths
rv_category_skip
rv_extract_tokens
rv_decide_verdict

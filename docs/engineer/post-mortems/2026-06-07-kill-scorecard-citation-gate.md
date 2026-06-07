# 2026-06-07 — Killed the scorecard citation gate

## What got deleted

- `scripts/check-scorecard.sh` (~235 LoC)
- `scripts/check-scorecard_test.sh` (~317 LoC)
- `scripts/testdata/memory/feedback_scorecard_evidence_token_required.md` (stub)
- `scripts/testdata/memory/feedback_scorecard_citation_token_outside_backticks.md` (stub)
- pr-lint workflow `check-scorecard` step
- Makefile `scorecard-check` target + `check-scorecard-test` target
- CLAUDE.md scorecard-row-label, citation-gate, evidence-token-shape bullets
- dispatch-templates A+ SCORECARD canonical block + CITATION GATE + LABEL GATE prose
- dispatch-templates recurring-failure-trap entries (#3, #5, #6, #8) about scorecard

## Why

The gate validated citation shape, not citation truth (post-mortem
2026-06-04-pr843-scorecard-alpine-busybox.md established this — `alpine:3.20`
cited in scorecard, `busybox:1.37` shipped in the diff; gate passed because
the token shape was right). For the self-host phase, the operator is the
sole author and reviewer; there is no second-tier vibes-grader the gate
catches. A token-shape gate adds friction (4 PR retries this session for
the trap class — see `feedback_pr_lint_body_snapshot_lag` cluster) without
catching the real failure mode (claim/diff divergence).

Per `feedback_deletion_default` and `feedback_default_simpler`: prefer
deletion to loosening. Per the self-host filter: "does the sole internal
operator need this to dispatch regatta-the-binary at this repo unattended?"
— no; the operator's own diff review catches truth-divergence faster than
the gate could.

## What stayed

- The B/A/A+ rubric concept (`feedback_grade_rubric`) — downgraded to
  "operator self-grades for own visibility, no CI gate, no required
  format". Specs MAY still ship rubric criteria; implementers MAY still
  self-rate. No machine check on the shape or presence.
- Adversarial reviewer subagent flow — still load-bearing per
  `feedback_adversarial_review`. Reviewer optionally re-scores the
  self-grade.

## Reopen-trigger

Bring the gate back when ANY of:

- External contributor lands a PR. (Multi-author = vibes-grading risk
  returns; need machine-checkable rubric again.)
- Audit need surfaces (compliance, customer pilot, billing-grade trail).
- Same misclaim pattern (cited-token-shape-right / diff-divergent) recurs
  ≥3 times within 30 days post-deletion.

When reopening, fetch the prior implementation from
git: the scorecard files were deleted in
`refactor/kill-scorecard-gate-self-host-deletion`. Recovery: `git log --
all -- scripts/check-scorecard.sh` then `git show <sha>:scripts/check-scorecard.sh > scripts/check-scorecard.sh`.

## Related memory

- `feedback_deletion_default` — every PR answers "what got smaller?"
- `feedback_default_simpler` — pick the simplest viable option.
- `feedback_grade_rubric` — rubric concept retained (downgraded).
- Prior post-mortem: `docs/engineer/post-mortems/2026-06-04-pr843-scorecard-alpine-busybox.md`

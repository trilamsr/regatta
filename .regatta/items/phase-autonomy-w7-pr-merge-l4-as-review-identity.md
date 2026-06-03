---
id: PHASE-AUTONOMY-W7
title: PR-merge L4-as-review identity — L4 ADOPT posts GH review with APPROVED state
lane: self-host
kind: feature
status: planned
gate: phase-autonomy-landing-3 (W2 merged)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W7
dependencies: PHASE-AUTONOMY-W2
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment §11 W7 (Landing 3, depends on W2 — the auto-merge call counts the L4-as-review against branch-protection's review-count requirement).

## Scope

L4 gate's ADOPT verdict becomes an actual GitHub PR review with `event=APPROVED`, so branch-protection's "≥1 approving review" count is satisfied. L4 REJECT becomes `event=REQUEST_CHANGES` with the failed-criteria list. Review body is deterministic: same PR + same gate state = byte-identical body.

**Two-identity model** (reconciled with [`docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`](../../docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md) §3 per `feedback_spec_pattern_authority`; closes #610): the original item-file framing ("operator's PAT signs the review", "no bot account introduced") was unsafe — GitHub returns 422 when `reviewer.login == pr.user.login`, and regatta opens PRs under a bot identity. The design subagent owns the deviation: a dedicated `regatta-reviewer-bot` PAT signs reviews, distinct from the author-side `regatta-bot` PAT. Both tokens flow through W6's credential store. Setup is encoded in `regatta install-service`.

## Approach

- Adopt the GitHub REST contract `POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews` verbatim.
- Reference `bors-ng` (Apache 2) — uses the same shape.
- Reference `github/safe-settings` (MIT) — branch-protection rules-as-config pattern.
- Build (~80 LoC) inside `internal/gates/l4`: post the review when ADOPT/REJECT fires + carry the per-criterion citation in the review body.
- Default-off; opt-in via `regatta.yaml: gates.l4_posts_review: true`.

## Acceptance criteria

- [planned] c1: L4 ADOPT verdict → `gh api repos/.../pulls/N/reviews` POST with `event=APPROVED`; body carries the per-criterion citation summary.
- [planned] c2: L4 REJECT verdict → POST with `event=REQUEST_CHANGES`; body carries the failed-criteria list.
- [planned] c3: Branch-protection "≥1 approving review" satisfied by the L4 review; W2 auto-merge proceeds.
- [planned] c4: Reviewer-bot identity (`regatta-reviewer-bot`) signs the review; distinct from the author-side bot PAT to avoid GH's 422 self-approval refusal. (Originally "operator's PAT, no bot account"; reconciled to two-identity model — see Scope.)
- [planned] c5: Review body is reproducible — same PR + same gate state = same review body verbatim.
- [planned] c6: Adversarial reviewer subagent posts.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c4 ship. (b) Release-notes fence. (c) Default-off; opt-in via `regatta.yaml: gates.l4_posts_review: true`. |
| A (target) | B + (d) c2+c3+c5+c6. (e) Replay harness shows review-body is byte-identical across two L4 runs against the same gate state. |
| A+ (stretch) | A + (f) When L4 changes verdict between runs, the prior review is dismissed via `PUT /pulls/N/reviews/{review_id}/dismissals` — no review accretion. (g) Per-criterion citations linked to substrate events (`/substrate/event/{id}` URL). (h) CODEOWNERS-aware: when CODEOWNERS demands a specific reviewer, L4 fails-closed instead of self-approving. |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W7
- GitHub REST `POST /pulls/N/reviews` — adopted contract
- `bors-ng` (Apache 2) — same-POST-shape reference
- `github/safe-settings` (MIT) — branch-protection rules pattern reference
- `feedback_decision_priority` — operator UX: no second account, no second login, no bot impersonation
- `feedback_research_design_principles` — adopt-first; REST contract adopted, gate wiring built
- `feedback_review_before_automerge` — L4-as-review keeps the "reviewer cleared" precondition explicit, not bypassed

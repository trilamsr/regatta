---
id: PHASE-AUTONOMY-W2
title: auto-merge-on-gate-pass — regatta serve calls gh pr merge --auto on green
lane: self-host
kind: feature
status: planned
gate: phase-autonomy-entry (Phase S3 closed)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W2
dependencies: PHASE-AUTONOMY-W1
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment §11 W2 (Landing 1, depends on W1 for the obs-alert interlock).

## Scope

Extend `cmd/regatta` serve so the substrate becomes the merge actor. When a PR's required checks turn green, L4 gate ADOPTs, cost-cap holds, and the adversarial reviewer has cleared, the serve loop calls `gh pr merge --squash --auto`. Operator no longer presses the merge button.

Config: `regatta.yaml: ci.automerge_on_pass: true` (default-off). Per-issue label `[auto-merge-ok]` bypasses L4-ADOPT for trivial doc PRs. Per-issue label `[needs-human-review]` blocks. Open `obs-alert` issue with severity `critical` halts all auto-merges substrate-wide.

## Approach

- Adopt `gh pr merge --auto` as the leaf mutation; no reimplementation of squash-merge.
- Build the policy engine inside `cmd/regatta` serve (~150 LoC): a `decideAutoMerge(pr) bool` function evaluated at each scheduler tick on PRs in CONCLUDED state.
- Reference `bors-ng` (Apache 2) for queueing/serialization; reference `Mergify` (Apache 2 OSS core) for the per-label override pattern.
- Substrate event `auto_merge_decision` emitted with the gate-result summary per PR.

## Acceptance criteria

- [planned] c1: Config `ci.automerge_on_pass: true` enables; default-off.
- [planned] c2: After PR closes-review + all required checks green + L4 ADOPT + cost-cap OK + adversarial reviewer cleared → `gh pr merge --squash --auto` fires.
- [planned] c3: Label `[needs-human-review]` blocks (escape hatch).
- [planned] c4: Label `[auto-merge-ok]` bypasses the L4-ADOPT requirement for trivial doc PRs (per `feedback_review_proportional`).
- [planned] c5: Open `obs-alert` issue with severity `critical` blocks all auto-merges substrate-wide until closed.
- [planned] c6: Adversarial reviewer subagent posts before merge fires.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2+c3 ship. (b) Default-off; explicit opt-in. (c) Release-notes fence. |
| A (target) | B + (d) c4+c5+c6. (e) Substrate event `auto_merge_decision` emitted with gate summary. (f) E2E test via mock GH server (e.g., `gock`) asserts merge-call shape. |
| A+ (stretch) | A + (g) Per-`obs-alert` severity interlock (only `critical` halts; `warning` allows). (h) Replay harness shows the gate-decision deterministic across 100 random PR states. (i) W5 cost-cap reset auto-unblocks queued merges atomically. |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W2
- `bors-ng` (Apache 2) — queueing/serialization reference
- `Mergify` (Apache 2 OSS core) — per-label override pattern
- `gh pr merge --auto` — adopted leaf mutation
- `feedback_review_proportional` — `[auto-merge-ok]` label honored for trivial doc PRs
- `feedback_decision_priority` — operator UX: substrate-as-merge-actor unblocks unattended night
- `feedback_research_design_principles` — adopt-first; merge command adopted, policy built

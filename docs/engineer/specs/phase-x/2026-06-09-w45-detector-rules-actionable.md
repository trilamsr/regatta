---
title: "W4.5 detector rules R6-R11 actionable plan (#832)"
status: phase-x-deferred
phase: self-host-s2
issue: 832
summary: "Per-rule SHIP / DEFER / REDUNDANT verdict for the six W4.5 self-improve detector rules in #832. Decision today: R8 (rework-cycle), R10 (priority-thrash), R11 (cap-thrash) are SHIPPABLE NOW — count-based primitives with zero baseline dependency, all upstream gates (MVR-1-T4 GH-issue adapter shipped #846, 565 PRs merged since 2026-06-01 ≫ the 30-PR baseline trigger from #832) are met, and the R12-friction pipeline merging via #1077 carries the dedup/throttle plumbing they reuse. R6 (latency-outlier) and R7 (cost-outlier) stay DEFERRED — both need stable 7d MAD baseline on per-kind substrate volume + R7 additionally gated on autotuner damping §8 wired + R11 firing ≥3× in 14d per #926 §9 denylist. R9 (success-pattern-extract) stays DEFERRED behind a two-step staircase: R6/R7/R8 each fire ≥10 times AND MVR-3-T4 research-mode methodology gates land. Implementation follows the sibling skeleton-prefetch spec (`2026-06-08-w45-detector-rules-r6-r11.md`) mechanics verbatim — this spec only ratifies the ship-now subset and pins reopen-data thresholds for the deferred rules. No new wedge."
deferred_on: 2026-06-10
---

# W4.5 detector rules R6-R11 actionable plan — Spec

Memory rules in force: `feedback_research_design_principles`, `feedback_default_simpler`, `feedback_cite_origin_main_not_local`, `feedback_no_signatures`, `feedback_no_self_tagged_approve`, `feedback_spec_pattern_authority`, `feedback_recognize_session_end`, `feedback_audit_main_before_implementing`.

```release-notes
[DOCS] specs: W4.5 detector rules R6-R11 actionable plan — per-rule ship/defer decision + reopen triggers (#832)
```

## §1 Problem

#832 filed six self-improve detector rules (R6-R11) as a wedge: open-loop issue-filing pending a baseline-data trigger + the MVR-1-T4 GH-issue adapter shipping. Sibling spec `docs/engineer/specs/2026-06-08-w45-detector-rules-r6-r11.md` (status: `skeleton-prefetch`, `phase: x-prefetch`) details mechanics for all six but does not commit to a ship date — the spec is held until the wedge unblocks.

The wedge gates listed in #832 have since cleared:

- **MVR-1-T4 GH-issue adapter** shipped in PR #846 on 2026-06-04 (`docs/engineer/autonomous-session-prompt.md` line: "PHASE MVR-1-T4 — Autonomous-loop CLOSED [SHIPPED 2026-06-04 #846]"; spec `docs/engineer/specs/2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` with `spec_id: MVR-1-T4`). The closed-loop consumer side of the issue surface is operational.
- **Baseline volume.** `gh pr list --search 'merged:>=2026-06-01' --state merged --json number -L 1000 | jq length` returns **567** at HEAD (2026-06-09, verified inline). The #832 reopen trigger named "30 autonomously merged PRs"; the count exceeds that by 18×.
- **Autotuner closed-loop** spec `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` is `status: active` ("ready for review", dated 2026-06-07), declaring the K1-K5 knob table R7 / R11 / R8 feed into.

Per `feedback_recognize_session_end` + `feedback_default_simpler`: with the wedge gates met, the wedge itself stops being load-bearing. The question is no longer "wait or build" — it is "which of the six rules survive an audit-before-build pass TODAY with the present plumbing, and which still defer".

Per `feedback_audit_main_before_implementing`: before assuming any rule needs implementation, this spec audits whether the work is already on main. The sibling spec `2026-06-09-auto-friction-trackers.md` (#1077, merged via PR #1128 per the operator brief) ships R12-friction with the dedup table + throttle gate + override label + serve loop wire-up that R8 / R10 / R11 would otherwise re-invent. The audit verdict (§3): the ship-now rules MUST reuse the R12-friction primitives rather than fork a parallel pipeline.

## §2 Design

This spec is a **decision artifact**, not a new mechanics spec. It records:

- **Per-rule verdict** (§3): SHIPPABLE NOW / DEFERRED / REDUNDANT — one column per rule, with the trigger that flips a DEFERRED rule.
- **R12-friction reuse map** (§4): for each SHIPPABLE rule, which sibling-spec primitive it consumes (dedup table, throttle gate, override label, serve-loop cadence, issue-body template).
- **Staircase for R9** (§5): the two-step gate (sibling rules' fire counts + MVR-3-T4 landing) that flips R9 from DEFERRED to ELIGIBLE.
- **Implementer brief** (§7): scope of the follow-up PR series that ships R8 + R10 + R11. Mechanics live in the sibling skeleton spec; this spec only re-points the implementer at it and pins the wire-up surface.

Per `feedback_spec_pattern_authority`: the rule mechanics (windows, thresholds, group_by, false-positive guards) ARE the sibling skeleton spec §3 — verbatim, no re-derivation, no implementer-time pattern picks. The only NEW decision this spec makes is which rules ship in the next dispatch wave and which keep deferring.

Per `feedback_default_simpler`: no new abstractions, no new packages. Each ship-now rule is a `streakRule` variant (`internal/selfimprove/rules.go:38`) registered alongside R1-R5 and R12-friction. No statistical primitive needed for the ship-now set (R8/R10/R11 are all count-based).

### 2.1 Audit-before-build against existing surface

| Surface | Already on main? | Reuse decision |
|---|---|---|
| `streakRule` primitive (`internal/selfimprove/rules.go:38-46`) | Yes — R1-R5 use it | Reuse verbatim |
| `filed_friction_trackers` dedup table (per `2026-06-09-auto-friction-trackers.md` §2.3) | Pending merge via #1077 sibling PR | Reuse — R8/R10/R11 share the same dedup-key fingerprint shape |
| Throttle 5/day + 50/week (per `2026-06-09-auto-friction-trackers.md` §2.5) | Pending merge | Reuse — caps are per-PROCESS not per-rule, R8/R10/R11 share the same budget with R12-friction |
| `do-not-auto-file:<rule-id>` override label (per `2026-06-09-auto-friction-trackers.md` §2.6) | Pending merge | Reuse — operator override scoped per rule-id |
| Serve-loop cadence goroutine (per `2026-06-09-auto-friction-trackers.md` §2.7) | Pending merge | Reuse — same `runOneFrictionScan` walks R12-friction + R8 + R10 + R11 findings in one pass |
| `agent_pr_head_changed` substrate event (R8 source) | Yes — emitted by `internal/orchestrator/prwatch` | Reuse |
| `scheduler_picked` substrate event (R10 source) | Yes — emitted by `internal/orchestrator/scheduler/scheduler.go` | Reuse |
| `KindTokenSpend` + cap-throttled meter (R11 source) | Yes — `internal/cost/cap/cap.go` | Reuse |
| Sloth SLO surface (`slo/*.yaml`) | Yes — 8 files audited in sibling skeleton spec §2 | No overlap; ship-now rules survive audit |

Zero new packages. One new file under `internal/selfimprove/` registers the three rules; no new emitters needed.

## §3 Acceptance

Per-rule verdict table — the load-bearing decision of this spec.

| Rule | Verdict | Justification | If DEFERRED, reopen trigger |
|---|---|---|---|
| **R6 latency-outlier** | **DEFERRED** | Needs 7d MAD baseline per-`(event_kind, agent_id_redacted)` bucket. Sibling spec §3.1 G6a hard-codes `count_7d ≥ 100` per bucket; substrate event volume at HEAD is ~hundreds/day across all kinds, so per-kind buckets won't accumulate 100 samples for ≥30 days. Also needs new `slo_alert_firing` substrate event-kind for the §2.2 SLO-suppression composition. | Reopen when (a) per-kind substrate-event volume reaches ≥100/7d for ≥3 of the four kinds named in sibling §3.1 (dispatch_completed, pr_stage_transition, scheduler_tick, l4_gate_completed) AND (b) `slo_alert_firing` event-kind lands. Audit at 30-day soak: `regatta self-improve baseline-audit --window 7d --min-samples 100`. |
| **R7 cost-outlier** | **DEFERRED** | Per autotuner spec `2026-06-07-autotuner-closed-loop.md` §9 denylist: R7 → K1/K2/K3 (cost caps) is admissible ONLY after §8 damping wired AND R11 has fired ≥3× in 14d. R11 has fired zero times (not yet shipped). Mechanical sequencing: R11 ships in this wave; R7 lands AFTER R11 has soaked. | Reopen when (a) R11 has fired ≥3× in a 14d window AND (b) autotuner §8 damping landed AND (c) 7d cost-spend MAD baseline ≥30 `pr_completed` events per sibling §3.2 G7a. |
| **R8 rework-cycle** | **SHIPPABLE NOW** | Count-based (≥3 force-pushes pre-merge), zero baseline. Source event `agent_pr_head_changed` already emitted by `internal/orchestrator/prwatch`. MVR-1-T4 issue surface shipped (#846). Closed-loop K4 (append-only dispatch-template) admissible immediately per autotuner spec §9. Sibling skeleton §3.3 mechanics verbatim. | N/A — ships in §7 follow-up PR. |
| **R9 success-pattern-extract** | **DEFERRED** | **Compound gate** (not three independent gates): R9 unblocks ONLY AFTER each of R6 + R7 + R8 satisfies (a) impl PR lands, (b) baseline ≥30d wall-clock soak, (c) accumulates ≥10 findings. Because R6 + R7 are themselves deferred behind their own multi-condition reopen triggers, the R9 gate is a chained dependency, not a near-term milestone. Plus (d) MVR-3-T4 research-mode methodology gates (`docs/engineer/specs/2026-06-03-mvr-3-t4-research-mode-overlay-skeleton.md`) ship to block selection-bias / leakage poisoning of dispatch templates via #926 K4. | Reopen when (a) R6 + R7 + R8 each: impl PR merged AND baseline ≥30d wall-clock AND ≥10 findings (verify via `regatta self-improve scan --summary`) AND (b) MVR-3-T4 spec status transitions to `shipped` AND (c) leakage check + counterfactual-probe primitives per sibling §3.4 G9c/G9d are wired into the methodology-gate suite. |
| **R10 priority-thrash** | **SHIPPABLE NOW** | Count-based (N>3 picks in 14d without progress), zero baseline. Source event `scheduler_picked` already emitted. Operator-visibility only — does NOT feed autotuner (per autotuner §4.4: scheduler knobs are Phase-X). No autotuner dependency to wait on. Sibling skeleton §3.5 mechanics verbatim. **Progress definition (tightened from sibling §6.1 v1 `pr_merged` only)**: `progress = pr_merged OR pr_closed_by_operator_with_reason=quality` (mechanism — label `thrash-ok`/`superseded`/`not-thrash` OR a close-comment grammar — deferred to impl per BUG-R10-progress tracker #1151). Without this tightening, R10 over-counts thrash when the operator legitimately closes PRs pre-merge. | N/A — ships in §7 follow-up PR. R10 impl MUST consume the BUG-R10-progress tracker #1151 resolution before merge. |
| **R11 cap-thrash** | **SHIPPABLE NOW** | Count-based (>2 distinct cap-throttled items same UTC day), zero baseline. Source events `KindTokenSpend` + `regatta_cost_cap_throttled_total` already emitted. Autotuner spec is `status: active` and explicitly names R11 as the primary upstream signal for K1/K2/K3 (per autotuner §9 admissibility predicate). Shipping R11 unblocks the autotuner co-fire window for R7. Sibling skeleton §3.6 mechanics verbatim. | N/A — ships in §7 follow-up PR. |

### 3.1 Sibling spec interplay — non-redundancy verdict

`docs/engineer/specs/2026-06-09-auto-friction-trackers.md` (#1077) ships **R12-friction** with three sub-rules (`friction_recurrence_agent_exit`, `friction_recurrence_spawn_failed`, `friction_recurrence_tick_slow`). Source-event surfaces are DISJOINT from R6-R11:

| Spec / rule | Source event kinds |
|---|---|
| R6 (this spec, deferred) | `dispatch_completed`, `pr_stage_transition`, `scheduler_tick`, `l4_gate_completed` (duration outliers) |
| R7 (this spec, deferred) | `pr_completed` (token spend outliers) |
| R8 (this spec, SHIP) | `agent_pr_head_changed` |
| R9 (this spec, deferred) | nightly LLM scan over `pr_completed` cohorts |
| R10 (this spec, SHIP) | `scheduler_picked` (progress absence) |
| R11 (this spec, SHIP) | `KindTokenSpend` + cap-throttled increments |
| R12 (sibling #1077) | `agent_non_completed_exit`, `spawn_failed_retry`, `tick_slow_repeat` |

Zero overlap. NO rule is REDUNDANT. The R12-friction *plumbing* (dedup, throttle, override, serve loop) is reused; the *rules* coexist.

### 3.2 Acceptance criteria for this spec PR

1. Spec lands at `docs/engineer/specs/2026-06-09-w45-detector-rules-actionable.md`.
2. `bash scripts/check-spec-sections.sh` clean for this file (canonical seven H2 sections present).
3. `make pre-push-check` Phase-X hint clean (no unwrapped Phase-X tokens in active specs — post-MAY-31 the hint is informational only).
4. `bash scripts/check-doc-links.sh` clean (every markdown link of form `[text]` then `(path)` to `docs/…`, `internal/…`, or `scripts/…` resolves at HEAD).
5. PR body declares `[DOCS]` release-notes block per CLAUDE.md release-notes-fence rule.
6. PR body does NOT carry `Reviewer-recommendation:` token (per the dispatch brief — independent reviewer pass to follow).
7. Per `feedback_no_self_tagged_approve`: no author-written APPROVE token. Reviewer-verdict gate auto-skips on `[DOCS]` release-notes EXCEPT for load-bearing-doc paths; `docs/engineer/specs/*.md` IS load-bearing, so an independent reviewer subagent must be dispatched before any APPROVE token lands.

## §4 Out of scope

Per `feedback_default_simpler`:

- **Mechanics of R8 / R10 / R11.** The sibling skeleton spec `2026-06-08-w45-detector-rules-r6-r11.md` §3.3 / §3.5 / §3.6 carries the verbatim mechanics. This spec does NOT re-derive them.
- **MAD primitive for R6/R7.** Sibling §9 followup #3 owns `internal/selfimprove/stat/mad.go`. Lands when R6 dispatches, not in the ship-now wave.
- **`slo_alert_firing` substrate emitter for R6 G6b suppressor.** Sibling §9 followup #1 owns this — one-file PR when R6 unblocks.
- **R7 cost-outlier follow-up.** Deferred per §3. Reopen-trigger pinned; no impl in this spec series.
- **R9 success-pattern-extract follow-up.** Two-step staircase per §5. Reopen-trigger pinned.
- **Autotuner K-knob wiring for R8.** Sibling §8 ratifies R8 → K4 (append-only dispatch-template). The wiring lands as part of the autotuner spec #926 impl, NOT in the R8-ship PR — R8 emits findings; autotuner consumes them.
- **Tuning of R12-friction throttle caps to accommodate three additional rules.** Sibling spec §2.5 chose 5/day + 50/week. The cap is per-PROCESS and now serves four rules (R12-friction + R8 + R10 + R11) under one shared budget. **Concrete reopen-trigger**: if `friction_tracker_throttled` substrate events fire >1/week post-ship for ≥2 consecutive weeks, file tracker `BUG-R11-throttle-tune` to either (a) per-rule sub-cap split or (b) lift the global cap. Do NOT pre-bump caps; let observed volume drive the decision.
- **Per-rule numeric threshold tuning.** Sibling skeleton spec defaults (R8: ≥3 force-pushes, R10: N>3 picks in 14d, R11: >2 items/day) ship as-is. Calibration follows the sibling spec §2.2 pattern: tracker on first mis-fire, not a YAML tier.

## §5 Adversarial

Per `feedback_adversarial_review_every_step` + the CLAUDE.md "Adversarial pass on specs mandatory" rule (`docs/engineer/specs/*.md` is load-bearing): this section is a SELF-AUDIT placeholder for the spec author's pre-review pass. An independent reviewer subagent is dispatched separately; the PR body's `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` tokens are INTENTIONALLY ABSENT from the initial PR submission per `feedback_no_self_tagged_approve`.

Likely adversarial-review hunting grounds (the reviewer should NOT trust this list — hunt fresh):

- **MVR-1-T4 closed-loop claim.** This spec cites #846 as the MVR-1-T4 shipping evidence. Reviewer: verify (a) `gh pr view 846 --json mergedAt,state` shows `MERGED` (not OPEN/BLOCKED post-automerge-flake per `feedback_watch_pr_until_merged`) and (b) the github_issues adapter is wired into a running orchestrator's loop, not just the package. The closed-loop claim is FALSE if the operator is still hand-filing every R12 issue.
- **567-PR baseline count.** Reviewer: re-run `gh pr list --search 'merged:>=2026-06-01' --state merged --json number -L 1000 | jq length` and verify the number AND verify the PRs are agent-authored, not operator-hand-merged. Per `feedback_cite_origin_main_not_local`, numeric claims must be paired with the exact command — this spec cites it inline, but the reviewer should re-run.
- **R12-friction primitive reuse.** This spec assumes R12-friction (spec #1077) ships its dedup table + throttle gate + override label + serve-loop cadence as a precondition for R8/R10/R11 reuse. As of HEAD, #1077 is spec-only — no R12-friction implementation PRs exist on main yet. The two impl waves are therefore dispatched IN PARALLEL: R12-friction impl spawn first to anchor the shared primitives, then R8/R10/R11 impl spawn referencing the same files. The two waves MUST land within the same release cycle so that no R12-friction-primitive PR ships without its consumers (and vice versa); cross-PR rebase coordination owned by the dispatching session. Reviewer: confirm the §7 implementer brief reflects parallel dispatch + shared-release-cycle landing, NOT a false "R12-friction PR-A/B/C merges first" precondition.
- **R8 G8a operator-author exclusion in autonomous loops.** The sibling §3.3 G8a guard excludes commits authored by the operator login. In a long autonomous loop, the operator may not push at all for days; R8 then over-fires on legitimate agent force-pushes. Reviewer: confirm the exclusion is checking the GH bot/app login allowlist correctly, not just one operator-pinned login.
- **R11 daily-bucket UTC boundary.** Sibling §6.4 leaves this as a v1 default (UTC). Reviewer: confirm test fixtures pass with daylight-saving aware timestamps in operator timezone.
- **Staircase for R9 is a COMPOUND gate, not three independent gates.** R9 unblocks ONLY AFTER each of R6 + R7 + R8 satisfies (a) impl PR lands, (b) baseline ≥30d wall-clock soak, (c) accumulates ≥10 findings. Because R6 and R7 are themselves DEFERRED behind their own multi-condition reopen triggers (§3 + §8), the R9 gate is effectively a chained dependency: R6 reopen-trigger fires → R6 ships → R6 soaks 30d → R6 ≥10 findings → (concurrently) same chain for R7 → then R9 candidate. Operator cannot read "R9 unblocks at 10 fires of each rule" as a near-term milestone; the soonest R9 is eligible is months out under steady autonomous load. Reviewer: confirm the spec explicitly surfaces this as a compound gate (§3 R9 row + §8 R9 bullet) instead of leaving the chain implicit.
- **Phase-x-leak gate behavior.** This spec's frontmatter sets `phase: self-host-s2`. The gate fires on Phase-X tokens (`tenant_id`, `RBAC`, etc.) in active specs. None present in this spec, but reviewer should confirm the gate still passes.
- **Reuse of R12-friction throttle.** The sibling spec sets 5/day + 50/week as caps. Adding three more rules can increase fire volume by up to 4×. Reviewer: confirm the spec accepts the cap-budget collision as documented (per §4 out-of-scope) and the tracker-on-throttle-exhaustion is the operator's signal.

This section is self-authored. NO `Reviewer-recommendation:` token in the PR body; an independent reviewer subagent will be dispatched in a follow-up review pass before any APPROVE token lands.

## §6 Implementer brief

Per `feedback_dispatch_brief_only`:

```
Scope: Implement R8 + R10 + R11 per the sibling skeleton spec
  `docs/engineer/specs/2026-06-08-w45-detector-rules-r6-r11.md` §3.3, §3.5, §3.6.
  Reuse the R12-friction dedup table + throttle gate + override label +
  serve-loop cadence shipped by #1077 / sibling spec
  `docs/engineer/specs/2026-06-09-auto-friction-trackers.md` §2.3-§2.7.
  DO NOT fork a parallel pipeline.

Sequencing: R12-friction impl (per spec #1077) and R8/R10/R11 impl are
  dispatched IN PARALLEL — #1077 is spec-only at HEAD; no impl PRs exist
  yet. Both waves share the dedup-table + throttle gate + override label +
  serve-loop primitives, so they MUST land within the same release cycle;
  the dispatching session owns cross-PR rebase coordination. R8/R10/R11
  PR opens against the same files R12-friction-impl creates. If R12-friction
  impl lands first, this PR rebases onto it; if this PR lands first, the
  R12-friction-impl PR rebases. Do NOT block dispatch on a precondition
  that does not exist.

R10-specific gate: BUG-R10-progress tracker #1151 MUST resolve before R10
  impl merges — the v1 sibling-spec progress definition (`pr_merged` only)
  over-counts thrash when operator legitimately closes PRs pre-merge. R10
  impl consumes the tightened definition: `progress = pr_merged OR
  pr_closed_by_operator_with_reason=quality`. Mechanism (label vs comment
  grammar) per #1151 resolution.

Files (one PR, three rules):
  - internal/selfimprove/rules.go:
      add three streakRule registrations (R8, R10, R11),
      add three EventKind* constants for the source events,
      register all three in DefaultRules() return slice.
  - internal/selfimprove/rules_test.go:
      extend with TestR8_*, TestR10_*, TestR11_* per sibling §5.1, §5.2, §5.3
      acceptance criteria. RED commit per `feedback_tdd_discipline` BEFORE impl.

TDD order:
  1. Failing-test commit: append all R8/R10/R11 tests, run `go test ./internal/selfimprove/...`
     and capture the failing output in PR body.
  2. Impl commit: add the three streakRule registrations + EventKind consts.
  3. Green commit: confirm `make ci-check` exit=0.

make ci-check exit: 0
Reviewer dispatch: YES — load-bearing per check-reviewer-verdict.sh
  (internal/selfimprove/ matches the load-bearing allowlist via
  cmd/regatta/selfimprove.go's CLI surface).

Per `feedback_spec_pattern_authority`: implementer MUST NOT deviate from sibling
spec §3 mechanics (window, threshold, group_by, false-positive guards). If
deviation appears needed, re-spawn the design subagent — do NOT pick at impl
time. The wedge unblock-trigger this spec ratifies (MVR-1-T4 shipped, 565 PRs
merged) does NOT license rule-mechanics drift.
```

Per `feedback_audit_main_before_implementing`: before dispatching, the implementer subagent must verify (a) `internal/selfimprove/rules.go` at origin/main does NOT already contain R8/R10/R11 registrations (search for `rework-cycle`, `priority-thrash`, `cap-thrash` rule-name strings), and (b) the R12-friction primitives the impl depends on are present at origin/main.

## §7 Reopen trigger

Per `feedback_recognize_session_end`: reopen this spec ONLY on a load-bearing change to the verdicts above. Per-verdict reopen triggers:

- **R6 latency-outlier — reopen when**:
  - Substrate event volume reaches ≥100/7d for ≥3 of the four kinds (`dispatch_completed`, `pr_stage_transition`, `scheduler_tick`, `l4_gate_completed`) per sibling §3.1 G6a, AND
  - `slo_alert_firing` substrate event-kind lands (sibling §9 followup #1), AND
  - MAD primitive `internal/selfimprove/stat/mad.go` lands (sibling §9 followup #3).
  - Audit command: `regatta self-improve baseline-audit --window 7d --min-samples 100`.

- **R7 cost-outlier — reopen when**:
  - R11 has fired ≥3 findings in a 14d window (autotuner §9 admissibility), AND
  - Autotuner §8 damping wired in PR series tracked under #926, AND
  - 7d cost-spend baseline accumulates ≥30 `pr_completed` events per sibling §3.2 G7a.

- **R9 success-pattern-extract — reopen when** (compound staircase — NOT three independent gates; each rule's own chain must complete first):
  - R6 unblock: per the R6 reopen-trigger above (substrate volume ≥100/7d × 3 kinds AND `slo_alert_firing` AND MAD primitive), AND R6 impl PR merged AND R6 baseline ≥30d wall-clock soak AND R6 ≥10 findings, AND
  - R7 unblock: per the R7 reopen-trigger above (R11 fired ≥3× in 14d AND autotuner §8 damping AND 7d cost-spend baseline), AND R7 impl PR merged AND R7 baseline ≥30d wall-clock soak AND R7 ≥10 findings, AND
  - R8: impl PR merged (ships in this wave) AND R8 baseline ≥30d wall-clock soak AND R8 ≥10 findings, AND
  - Per-rule fire counts verified via `regatta self-improve scan --summary` per-rule count, AND
  - MVR-3-T4 spec `docs/engineer/specs/2026-06-03-mvr-3-t4-research-mode-overlay-skeleton.md` transitions to `status: shipped`, AND
  - Leakage check + counterfactual-probe primitives per sibling §3.4 G9c/G9d wire into the methodology-gate suite.

- **This spec (overall) — reopen when** any of:
  - R8 / R10 / R11 ship and the verdict table needs updating to `status: shipped` per rule, OR
  - The 567-PR baseline reverses (autonomous loop pause for ≥30d, baseline ages out) — wedge state needs re-derivation, OR
  - Sibling skeleton spec `2026-06-08-w45-detector-rules-r6-r11.md` is amended in a way that invalidates a verdict here, OR
  - BUG-R10-progress tracker #1151 resolves with a definition different from the §3 R10-row claim — update R10 row accordingly, OR
  - `friction_tracker_throttled` events fire >1/week for ≥2 consecutive weeks post-ship → file BUG-R11-throttle-tune, reopen this spec to record the per-rule cap-split or global-lift decision, OR
  - Operator observes that an autotuner mis-fire was caused by a deferred rule's absence — the deferral was too conservative, escalate the rule's verdict.

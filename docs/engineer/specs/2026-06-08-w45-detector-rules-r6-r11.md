---
title: "W4.5 self-improve detector rules R6-R11 — Design Spec"
status: skeleton-prefetch
phase: x-prefetch
summary: "Detail spec for the six W4.5 detector rules (R6 latency-outlier, R7 cost-outlier, R8 rework-cycle, R9 success-pattern-extract, R10 priority-thrash, R11 cap-thrash) extending W4's `internal/selfimprove/rules.go` MVP set. Spec is BASELINE-GATED (wedge): no baselines exist yet for std-dev or median computations, autotuner closed-loop (#926) is required for R7/R11 to close, and the research-mode overlay (MVR-3-T4) overlaps R9. Audit-before-build pass confirmed all six rules survive against the existing Sloth SLO surface — SLOs alert on aggregate p95 budget burn, R6-R11 detect per-event outliers + cross-event patterns that fire BEFORE the SLO budget tips, so the surfaces compose rather than duplicate. Reopen-trigger: 30 days of substrate event data (R6/R7 baseline window) AND #926 autotuner spec merged. R8 lands first when wedge unblocks — it is the only rule whose threshold is count-based (≥3 force-pushes), needs zero baseline, and emits signal pre-autotuner."
---

# W4.5 self-improve detector rules R6-R11 — Design Spec

Status: skeleton-prefetch (baseline-gated wedge per #832)
Date: 2026-06-08
Author: design subagent
Source issue: #832 (W4.5 wedge, operator-deferred)
Parent spec: `docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md`
Closed-loop destination: `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` (#926)
Existing rules: `internal/selfimprove/rules.go:13-17` (R1-R5)

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`.

---

## §1 Problem

W4 shipped five rules (R1-R5 in `internal/selfimprove/rules.go:13-17`) that fingerprint a fixed string tuple (e.g. `gate_kind+gate_reason`) and count occurrences in a sliding window. The shape works because every R1-R5 trigger is a discrete repeat — "the same banned token tripped twice", "the same gate fired three times". Count + group_by + window is sufficient.

#832 lists six rules the operator hit in observed waves whose shape is NOT a discrete repeat:

- **R6 latency-outlier** — a single dispatch tail-spike worth investigating BEFORE it pushes the SLO p95 over budget.
- **R7 cost-outlier** — a single PR run that burned 5× the usual token budget; pattern surface for the autotuner (#926 K1/K2/K3).
- **R8 rework-cycle** — a PR that force-pushed ≥3 times before merge; signals a quality-feedback gap in the dispatch brief.
- **R9 success-pattern-extract** — the inverse of failure: PRs in the fast+cheap p10 share a prompt feature that should be templated.
- **R10 priority-thrash** — scheduler keeps picking the same item without progress; signals a planner defect, not an agent failure.
- **R11 cap-thrash** — cost-cap tripped on N>2 items in one day; signals the cap is wrong, not that agents misbehaved.

Each requires either a statistical-baseline computation (R6, R7, R9) or a cross-event correlation across multiple substrate kinds (R8, R10, R11). The W4 `streakRule` primitive in `rules.go:38-46` cannot express either shape — its `groupBy` returns a `map[string]string` for exact-match bucketing only.

This spec extends the rule library to cover both shapes, audits each rule against the existing Sloth SLO surface (`slo/*.yaml`) so we do NOT duplicate alerts, sequences which rule ships first when the wedge unblocks, and pins the closed-loop integration points with the autotuner spec (#926).

### 1.1 Non-goals

- Computing or pinning thresholds. Per #832 deferral rationale, no baseline data exists yet. Threshold defaults live in YAML and are calibrated post-soak. This spec defines the rule MECHANICS only.
- Replacing W4 rules. R6-R11 extend the rule registry; R1-R5 keep current shape.
- Building the autotuner. #926 owns the consumer side. This spec lists which R6-R11 findings feed which autotuner knob.
- General anomaly detection. Each new rule is a NAMED pattern with a specific signal source.

---

## §2 Audit-before-build — survival against existing SLO surface

Per `feedback_default_simpler`: a detector rule that duplicates an existing Sloth alert MUST be rejected. Inventory of `slo/*.yaml` at HEAD:

| SLO file | Alert | Signal | Threshold |
|---|---|---|---|
| `slo/dispatch-subagent.yaml` | DispatchSubagentLatencyHigh | `regatta_dispatch_subagent_duration_seconds_bucket` | p95 ≤ 120s / 7d |
| `slo/pr-lifecycle.yaml` | PRLifecycleStageLatencyHigh | `regatta_pr_stage_duration_seconds_bucket` | p95 ≤ 3600s / 30d |
| `slo/scheduler-tick.yaml` | SchedulerTickLatencyHigh | `regatta_scheduler_tick_latency_ms` | p95 budget burn |
| `slo/l4-latency.yaml` | L4GateLatencyHigh | L4 gate duration histogram | p95 budget burn |
| `slo/replay-latency.yaml` | ReplayLatencyHigh | replay duration histogram | p95 budget burn |
| `slo/substrate-chain-break.yaml` | SubstrateChainBreakDetected | HMAC chain-break counter | any |
| `slo/substrate-divergence.yaml` | SubstrateDivergenceDetected | divergence counter | any |
| `slo/substrate-event-rate.yaml` | SubstrateEventRateAnomaly | event-rate counter | sliding-window stall |

### 2.1 Per-rule audit verdict

| # | Rule | Closest SLO | Overlap? | Verdict |
|---|---|---|---|---|
| R6 | latency-outlier (>3σ on 7d-median by kind) | DispatchSubagentLatencyHigh, SchedulerTickLatencyHigh, L4GateLatencyHigh, PRLifecycleStageLatencyHigh | partial — SLOs alert on aggregate p95 budget burn | **survives** with §2.2 framing |
| R7 | cost-outlier (>3σ tokens on 7d median) | none | none — cost meters exist (`regatta_cost_cap_24h_spend_usd`, `regatta_cost_cap_throttled_total`) but no Sloth YAML pages on them | **survives** clean |
| R8 | rework-cycle (≥3 force-pushes pre-merge) | none | none — PR lifecycle SLO measures stage DURATION, not REWORK COUNT | **survives** clean |
| R9 | success-pattern-extract (p10 fast+cheap PRs share feature) | none | none — SLOs don't measure success patterns | **survives**, but see §2.3 MVR-3-T4 overlap |
| R10 | priority-thrash (scheduler picks same item N>3 without progress) | SchedulerTickLatencyHigh | none — scheduler latency ≠ priority churn | **survives** clean |
| R11 | cap-thrash (cost-cap hit on N>2 items same day) | none | none — meter exists, no alert | **survives** clean |

**Zero rules dropped.** All six survive — R6 with explicit framing (§2.2), R9 with overlap note (§2.3).

### 2.2 R6 framing — outlier ≠ p95 budget burn

`DispatchSubagentLatencyHigh` fires when 7d burn rate of the 120s-p95 budget exceeds Sloth's default 5% multi-window threshold. By the time the page fires, the budget is already burning. R6 detects a SINGLE event whose duration is >3σ above the 7d median for the same `event_kind` — a tail-spike that has NOT yet moved p95 enough to trip the SLO but IS the operator-visible "why did one dispatch take 30 min" question.

R6 fingerprint: `(event_kind, agent_id_redacted)`. Output: one self-improvement issue per outlier event with citations to the substrate row + the p95 + the σ. Per-event surface; SLO is per-window.

Composition rule: if `DispatchSubagentLatencyHigh` is already PAGE, R6 SUPPRESSES — the SLO alarm dominates. R6 fires only when SLO is GREEN. Coordination via the existing `filter_out` field on `streakRule` (extension: §3.4 below adds substrate-event-kinds `slo_alert_firing`).

### 2.3 R9 overlap — MVR-3-T4 research-mode

`docs/engineer/specs/2026-06-03-mvr-3-t4-research-mode-overlay-skeleton.md` introduces `WorkItem.kind=research` + four methodology gates. R9's "success pattern extraction" reads like a research-mode finding — "PRs that share feature X are 3× faster". The risk: R9 emits a `feedback_*` proposal that the autotuner appends to a dispatch template (#926 K4); if the underlying inference was a research-mode methodology violation (selection bias, leakage), the template gets poisoned with a spurious pattern.

Mitigation: R9 is **gated on MVR-3-T4 landing**. Until then, R9 emits LLM-nightly proposals only (per W4 spec §7) — NOT issues, NOT autotuner inputs. After MVR-3-T4 lands, R9 may file an issue if the candidate cluster passes the methodology-gate suite. This puts R9 strictly behind the autotuner integration; R9 + #926 closed loop ships LAST in the sequencing (§4).

---

## §3 Rule mechanics — per-rule detail

### 3.1 R6 — latency-outlier

**Trigger condition.** For each `substrate_event` row whose `kind ∈ {dispatch_completed, pr_stage_transition, scheduler_tick, l4_gate_completed}`, compute `z = (duration - median_7d) / mad_7d` where `mad_7d` is median-absolute-deviation over the prior 7d window for the same `(kind, agent_id_redacted)` fingerprint. Fire if `z > 3.0` AND no `slo_alert_firing` event for the same `kind` in the prior 1h (§2.2 suppression).

**Signal source.** `substrate_events.payload_json -> duration_ms` field already populated by `internal/obs/dispatch` (OBS-WAVE-C-T1) + `internal/orchestrator/state/substrate` PR-stage emitter (OBS-WAVE-C-T2) + `internal/orchestrator/scheduler/scheduler.go:281` + `internal/gates/l4/metrics.go`. NO new instrumentation.

**Threshold computation (DEFERRED — calibration).** `z > 3.0` is a default; calibration after 30-day soak. YAML-overridable `z_threshold` field; per-kind override `z_threshold_per_kind` map. MAD computed in Go (no new dep) — `internal/selfimprove/stat/mad.go`.

**False-positive guards.**
- **G6a Cold-start**: if `count_7d < 100` for a given `(kind, agent_id_redacted)` bucket, skip (insufficient baseline). Default minimum sample size.
- **G6b SLO-paging suppressor**: if `kind`-matching SLO is currently PAGE, skip. Reads `slo_alert_firing` substrate events (NEW — §3.4 backlog item).
- **G6c Pause-window suppressor**: existing `PauseAllTag` filter from R1-R5 applies (`rules.go:23`).
- **G6d Distinct-PR-count gate**: outlier must affect ≥2 distinct PRs / runs in the same `kind` over the prior 24h — single ill-timed event is not yet a pattern.

**Closed-loop destination.** Per #926 §6: R6 findings DO NOT feed the autotuner K1-K5 in v1. R6 is operator-visibility only. Future K6 (per-`event_kind` latency-budget knob) is a Phase-X reopen.

### 3.2 R7 — cost-outlier

**Trigger condition.** For each `pr_completed` substrate event (or equivalent terminal-state event), extract `tokens_total` from payload. Fire if `tokens_total > median_7d + 3 * mad_7d` AND the PR's spend pushed `regatta_cost_cap_24h_spend_usd` above 50% of `safety.cost.cap.daily_usd`.

**Signal source.** Existing `KindTokenSpend` substrate events (`internal/orchestrator/state/substrate/event.go`). NO new meter.

**Threshold computation (DEFERRED).** 7d median + 3·MAD. Operator-overridable in YAML. Same `internal/selfimprove/stat/mad.go` primitive as R6.

**False-positive guards.**
- **G7a Cold-start**: skip if fewer than 30 `pr_completed` events in the 7d window (autonomous loop low-volume early days).
- **G7b Spec-PR exclusion**: filter `pr_kind = docs|chore|ci` — large doc PRs legitimately burn tokens.
- **G7c Cost-cap-state suppressor**: if `regatta_pause_all` was active during the PR's lifecycle (existing W5 surface), skip — cap-induced retries inflate token count and are NOT an agent defect.
- **G7d Distinct-PR-window gate**: at least 1 distinct repeat (2 outlier PRs same week sharing a fingerprint) before filing — single outlier might be a one-off task complexity.

**Closed-loop destination.** Per #926 K1/K2/K3 (cost-cap raise/lower) AND #926 §9 autotuner denylist EXCLUDES R7 from auto-firing until §8 damping wired AND R11 fired ≥3× in 14d (de-noise). R7 + R11 co-fire is the autotuner trigger; R7 alone is operator-visibility only.

### 3.3 R8 — rework-cycle (lands first per §4)

**Trigger condition.** For each PR row in substrate (or `pr_force_push` event kind, NEW — §3.4 backlog), count force-pushes between `pr_opened` and `pr_merged` (or `pr_closed`). Fire if `force_pushes ≥ 3` AND PR eventually merged (rework that succeeded — distinguishes from PR-abandoned).

**Signal source.** GitHub `head_sha` change history is already polled by `internal/orchestrator/prwatch` and emitted as `agent_pr_head_changed` substrate events. R8 counts these per `pr_number`. Existing event surface, no new emitter required.

**Threshold computation (NO baseline needed).** Count-based — no median, no σ. Default threshold = 3 per the issue body. Operator-overridable.

**False-positive guards.**
- **G8a Operator-author exclusion**: if the head_sha author is the operator's own login (not the agent bot), skip — operator hand-fixes are not a quality-feedback gap.
- **G8b Auto-rebase exclusion**: head_sha changes triggered by `gh pr update-branch` / GitHub auto-rebase do NOT carry agent intent — filter via the rebase commit's parent shape (single parent on origin/main, no agent-bot author).
- **G8c First-day-of-feature exclusion**: skip PRs whose first force-push happened <60s after open (CI-feedback cycle, not rework).

**Closed-loop destination.** R8 findings feed #926 K4 (dispatch-template append-only) — the suggested edit is "add to implementer brief: when X fails on first push, do Y." Most signal-rich pre-baseline rule because the threshold is a count, not a percentile.

### 3.4 R9 — success-pattern-extract (gated on MVR-3-T4)

**Trigger condition.** Once per nightly LLM scan (existing W4 §7 cron). Compute the p10 of `(merge_latency, total_tokens)` PRs over the prior 30 days. Cluster their prompt-features (dispatch-template lines, briefing slugs, file scope). Fire if a cluster of ≥5 p10-PRs share a feature NOT present in the p50 cohort.

**Signal source.** Existing `pr_completed` substrate events + the W4 LLM-nightly digest pipeline (`prompts/self-improvement-scan.txt`).

**Threshold computation (DEFERRED).** Cluster size ≥5, feature-novelty test = "present in ≥80% of p10 cohort AND ≤20% of p50 cohort". Calibration after 60-day baseline.

**False-positive guards.**
- **G9a Methodology-gate dependency**: BLOCKED until MVR-3-T4 lands. Until then, R9 emits LLM proposals (file at `internal/selfimprove/proposals/`), never issues, never autotuner inputs.
- **G9b Sample-size gate**: skip if <30 PRs in p10 cohort OR <100 in p50 (selection bias risk).
- **G9c Leakage check**: feature must be authored BEFORE the PR opened — features extracted from PR body / agent post-hoc commentary are leakage.
- **G9d Counterfactual probe**: cluster passes only if a hold-out PR from the p10 cohort with the feature manually masked falls back to p50 latency — quantified via the MVR-3-T4 leakage gate.

**Closed-loop destination.** Per #926 K4 ONLY (dispatch-template append). NEVER K1/K2/K3 (cost) or K5 (banned-phrase). R9 + autotuner ships LAST in sequence (§4).

### 3.5 R10 — priority-thrash

**Trigger condition.** For each `work_item.id` whose `scheduler_picked` substrate event fires N>3 times in a 14d window AND no `work_item.status = done` event lands in the same window. Fire.

**Signal source.** Existing `scheduler_picked` events (`internal/orchestrator/scheduler/scheduler.go`). NO new emitter.

**Threshold computation (NO baseline needed).** Count-based — N>3 picks, 14d window. Operator-overridable.

**False-positive guards.**
- **G10a Progress definition** — see §6 open question. v1 progress definition (deferred-via-default): an item is *resolved* (NOT thrash) when a `pr_merged` event OR an operator-actor `pr_closed` event (`merged=false`, `closed_by_actor=operator`) lands for the same `work_item.id`. An operator closing a PR pre-merge — work pivoted, approach wrong, issue reclassified — is a legitimate terminal state, not churn. Only an *agent-* or *auto-*closed `pr_closed(merged=false)` followed by a re-pick IS the signal R10 is hunting; count THAT toward N. A bare `pr_opened` between picks does NOT count as progress on its own (a PR can open then agent-close then re-pick).
- **G10b Operator-pin exclusion**: if the work_item is operator-pinned (frontmatter `pin: true`), skip — operator chose to keep it on deck.
- **G10c Cold-start**: skip if `work_item.created_at` is within the prior 24h (legitimate retry burst).

**Closed-loop destination.** Per #926 §9 — R10 is operator-visibility only in v1. The autotuner cannot adjust scheduler priority knobs (Phase-X per #926 §4.4). Future R10 + autotuner integration requires a new K knob in #926 K6+.

### 3.6 R11 — cap-thrash

**Trigger condition.** For each calendar day (UTC), count distinct `work_item.id`s whose lifecycle included a `regatta_cost_cap_throttled_total` increment. Fire if `count_per_day > 2`.

**Signal source.** Existing `KindTokenSpend` + cap-throttled emitter (`internal/cost/cap/cap.go:148`). NO new meter.

**Threshold computation (NO baseline needed).** Count-based — `>2` distinct items in one calendar day.

**False-positive guards.**
- **G11a Pause-window suppressor**: if `regatta_pause_all` was active during any of the throttled events, skip — pause-induced throttles aren't cap-mis-fit.
- **G11b Same-feature exclusion**: if all throttled items share the same `dispatch_template + work_item.kind` AND the autotuner just minted a K4 dispatch-template change in the prior 24h, skip — let the K4 change soak before re-firing.
- **G11c Per-item-attempts gate**: each counted item must have ≥2 attempts (multiple sub-runs in the same day) — single attempt that tripped cap is a one-off, not thrash.

**Closed-loop destination.** Per #926 K1/K2/K3 — R11 is the EXPLICIT trigger for `cap-thrash` autotuner action per #926 §9 ("Admissible ONLY when §8 wired AND R11 has fired ≥3 times in a 14-day window"). R11 is the autotuner's primary upstream signal once R7 + R11 co-fire predicate is met.

---

## §4 Sequencing — which rule lands first

Per #832 body: "R8 is most signal-rich pre-baseline." This spec ratifies that with sequencing rationale.

| Order | Rule | Why first / why later |
|---|---|---|
| 1st | **R8 rework-cycle** | Count-based, zero baseline needed. Existing `agent_pr_head_changed` event already emitted. Feeds #926 K4 (append-only dispatch-template). Lowest-risk first feed for the autotuner. |
| 2nd | **R10 priority-thrash** | Count-based, zero baseline. Existing `scheduler_picked` events. Operator-visibility only (#926 cannot adjust scheduler knobs in v1) — safe to ship without autotuner. |
| 3rd | **R11 cap-thrash** | Count-based, zero baseline. Once R11 has fired ≥3× in 14d, R7 becomes admissible (per #926 §9 denylist). Sequencing R11 before R7 is mechanical. |
| 4th | **R6 latency-outlier** | Requires 7d MAD baseline (30d for stability). Requires new `slo_alert_firing` event kind for suppression (§3.4 backlog). Calibration soak before going beyond LLM-nightly proposals. |
| 5th | **R7 cost-outlier** | Requires same MAD primitive as R6 + R11 prior fires. Closed-loop with #926 K1/K2/K3 is the value-add — ship after R11 + autotuner damping (§926 §8) are both wired. |
| 6th (last) | **R9 success-pattern-extract** | Blocked on MVR-3-T4 methodology gates. Ships LAST. LLM-nightly proposals only until MVR-3-T4 merges. |

The wedge unblock-trigger from #832 (30 days substrate data + #926 autotuner spec merged) gates the entire sequence. R8 + R10 can technically land before the wedge unblocks (zero baseline), but the closed-loop benefit dominates — ship them as part of the wedge.

---

## §5 Acceptance criteria — per rule

Each rule lands as a single-file PR + a single Go test file extending `internal/selfimprove/rules_test.go` per W4 spec §5.3 ("adding a 6th rule = one-file PR + one Go test"). Per `feedback_tdd_discipline`, every test below lands as a FAILING commit before its impl.

### 5.1 R8 acceptance (first)

- **T_R8_a** `TestR8_RuleFiresOnThreeForcePushesPreMerge` — fixture event stream with 3 `agent_pr_head_changed` events + 1 `pr_merged` event ⇒ Finding emitted. RED first.
- **T_R8_b** `TestR8_FalsePositiveOperatorAuthorExcluded` — head_sha author = operator login ⇒ no Finding. RED first.
- **T_R8_c** `TestR8_FalsePositiveAutoRebaseExcluded` — head_sha change with rebase-shape parent ⇒ no Finding. RED first.
- **T_R8_d** `TestR8_DedupKeyStable` — re-run on same fixture ⇒ same dedup key (parity with R1-R5).

### 5.2 R10 acceptance (second)

- **T_R10_a** `TestR10_RuleFiresOnFourPicksWithoutProgress` — 4 `scheduler_picked` events same `work_item.id`, no `pr_opened`+`pr_merged` between picks ⇒ Finding. RED first.
- **T_R10_b** `TestR10_FalsePositiveOperatorPinExcluded` — work_item with `pin: true` frontmatter ⇒ no Finding. RED first.
- **T_R10_c** `TestR10_AgentClosePreMergeIsThrash` — pick → pr_opened → pr_closed(merged=false, closed_by_actor=agent) → pick × 4 ⇒ Finding (an agent/auto close-without-merge followed by re-pick IS the thrash signal).
- **T_R10_d** `TestR10_OperatorClosePreMergeResolves` — pick → pr_opened → pr_closed(merged=false, closed_by_actor=operator) → pick × 4 ⇒ no Finding (an operator pre-merge close is a legitimate terminal state per §6.1 G10a — resolved, not churn). RED first.

### 5.3 R11 acceptance (third)

- **T_R11_a** `TestR11_RuleFiresOnThreeCapHitsSameDay` — 3 distinct work_items, each with `regatta_cost_cap_throttled_total` increment, same UTC day ⇒ Finding. RED first.
- **T_R11_b** `TestR11_FalsePositivePauseWindowExcluded` — `regatta_pause_all` event active during throttles ⇒ no Finding.
- **T_R11_c** `TestR11_AutotunerCoFirePredicate` — R11 fires 3× in 14d ⇒ Finding payload carries `autotuner_admissible: true`. Maps to #926 §9 denylist exit.

### 5.4 R6 acceptance (fourth)

- **T_R6_a** `TestR6_FiresOnThreeSigmaTailEvent` — fixture 100-event baseline, single event 4σ above median ⇒ Finding. RED first.
- **T_R6_b** `TestR6_ColdStartGateSkipsFewerThanHundred` — 50-event baseline ⇒ no Finding (G6a).
- **T_R6_c** `TestR6_SLOPagingSuppressorSkips` — `slo_alert_firing` event for same kind in prior 1h ⇒ no Finding (G6b / §2.2 composition).
- **T_R6_d** `TestR6_DistinctPRCountGate` — outlier event affecting only 1 PR ⇒ no Finding (G6d).

### 5.5 R7 acceptance (fifth)

- **T_R7_a** `TestR7_FiresOnThreeSigmaTokenSpend` — 30-PR baseline, single PR 4σ over median tokens AND cap-spend >50% ⇒ Finding. RED first.
- **T_R7_b** `TestR7_DocsPRExcluded` — `pr_kind = docs` ⇒ no Finding (G7b).
- **T_R7_c** `TestR7_PauseWindowSuppressorSkips` — `regatta_pause_all` during PR lifecycle ⇒ no Finding (G7c).

### 5.6 R9 acceptance (sixth, gated)

- **T_R9_a** `TestR9_GatedOnMVR3T4_EmitsProposalNotIssue` — until MVR-3-T4 spec status is `shipped`, R9 emits a proposal file at `internal/selfimprove/proposals/` AND zero issues. RED first.
- **T_R9_b** `TestR9_LeakageCheckRejectsPostHocFeature` — feature derived from `pr_body_scan` event firing AFTER `pr_opened` ⇒ proposal rejected with `leakage` reason.
- **T_R9_c** `TestR9_SampleSizeGateSkipsSmallP10` — p10 cohort size <30 ⇒ skip.

---

## §6 Open questions — decision-required

Per #832 body and the brief's pattern (`feedback_unaddressed_load_bearing`): each question carries a default for v1, reopens at wedge-unblock.

### 6.1 What counts as "progress" for R10?

**Issue.** R10 fires when an item is picked N>3 times without progress. The naive definition (`pr_opened` event lands between picks) over-counts — a PR can be opened, closed without merge, re-picked. A `pr_merged`-only definition *under-counts the inverse way*: it miscounts an operator-legitimate pre-merge close (work pivoted, approach wrong, issue reclassified) as thrash, even though the operator deliberately resolved the item. Both terminal states — merge AND operator-close — are progress.

**v1 default (carried by §3.5 G10a):** an item is *resolved* (NOT thrash) when EITHER a `pr_merged` event OR an operator-actor `pr_closed(merged=false, closed_by_actor=operator)` event lands for the same `work_item.id`. Distinguishing the closer's actor is the load-bearing field: an operator closing a PR pre-merge is a valid decision, whereas an *agent-* or *auto-*closed `pr_closed(merged=false)` followed by a re-pick is the thrash signal R10 is hunting and DOES count toward N. A bare `pr_opened` between picks does not count as progress on its own.

**Actor source.** `closed_by_actor` derives from the `pr_closed` substrate event's GitHub `closed_by` login vs. the orchestrator's known agent-bot identities: a close by the operator login (not a regatta agent bot, not GitHub automation) is `operator`. If the substrate `pr_closed` event lacks a closer identity at wedge-unblock, default `closed_by_actor=agent` (fail toward counting — preserves the original thrash-hunting posture) and file a follow-up to add closer-identity capture to the `pr_closed` emitter.

**Reopen-trigger:** if R10 still fires on items the operator legitimately resolved through some terminal state OTHER than merge-or-operator-close (e.g. the item was reclassified into another work_item and the original spec was marked `shipped`/`archived` without ever opening a closing PR), this default needs broadening. File tracking issue on first such observation. Possible refinement: also count `spec_status:shipped | item_archived` as progress.

### 6.2 R6 / R7 baseline calibration window

**Issue.** §3.1 / §3.2 default to 7d window for median + MAD. Substrate event volume is currently ~hundreds/day (per W4 spec §9.1). At that rate, 7d gives ~thousands of samples per `kind` — borderline for stable MAD.

**v1 default:** 7d window, 30-day soak before going beyond LLM-nightly proposals. After 30 days, audit MAD stability per-kind; if MAD jitter >25% week-over-week, extend to 14d.

### 6.3 R8 force-push counter — events vs poll-derived

**Issue.** R8 counts `agent_pr_head_changed` events. `internal/orchestrator/prwatch` emits these on every detected SHA change but does NOT distinguish force-push from fast-forward push.

**v1 default:** any `agent_pr_head_changed` event counts toward the rework-cycle counter. The G8b auto-rebase exclusion filters most noise. If false-positive rate is high after 30d, refine prwatch to emit a separate `pr_force_push_detected` event (compares before/after SHA tree-hash).

### 6.4 R11 daily-bucket — UTC vs operator timezone

**Issue.** R11 counts cap-hits per CALENDAR DAY. UTC midnight may split a single thrash incident across two buckets.

**v1 default:** UTC. Operator timezone introduces test-determinism risk per `feedback_windows_path_tests` shape (timezone-dependent assertions). If a real thrash incident gets missed because it straddled midnight UTC, refine to a sliding 24h window. Tracking issue filed at first observed split-miss.

---

## §7 Out of scope — explicit non-goals

Per `feedback_default_simpler` + `feedback_deletion_default`:

| Out of scope | Rationale |
|---|---|
| **Thresholds** — actual numeric defaults pinned in YAML | No baseline data; per §1.1, calibration deferred until soak. v1 ships with conservative defaults (z>3.0, count>3) tagged `# CALIBRATE-2026-Q3` in YAML. |
| Statistical libraries (gonum, Apache Commons Math) | MAD + median over <10k samples = O(n log n) in stdlib `sort.Slice` + `sort.SearchInts`. No new dep. `internal/selfimprove/stat/mad.go` is ~30 LoC. |
| Sloth SLO YAML additions for R6-R11 findings | Per #926 §A13 ("no new SLO YAMLs required"): R6-R11 file issues + feed autotuner. SLO surface stays operator-paging; detector surface stays operator-issue-filing. |
| Real-time streaming detection | Detector runs on the W4 6h cron + nightly LLM (W4 §8.2). R6/R7/R11 latency-to-finding is minutes-to-hours, not seconds. |
| Cross-rule correlation engine | R7+R11 co-fire predicate (§3.6 / §5.3 T_R11_c) is the ONE cross-rule check, handled inline. General correlation is Phase-X. |
| Operator-tunable z-threshold per agent_id | Per-kind override map is the v1 surface. Per-agent_id is Phase-X — wait for the second internal operator. |

---

## §8 Closed-loop integration — autotuner #926

Per `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` §6 (K-knob table) + §9 (denylist):

| Rule | Autotuner knob fed | Autotuner gating |
|---|---|---|
| R6 | none in v1 | future K6 Phase-X reopen |
| R7 | K1/K2/K3 (cost caps) | gated on R11 co-fire ≥3× in 14d (#926 §9) |
| R8 | K4 (dispatch-template append) | admissible immediately (append-only, §6 fence) |
| R9 | K4 (dispatch-template append) | gated on MVR-3-T4 methodology gates landing |
| R10 | none in v1 | scheduler knobs Phase-X (#926 §4.4) |
| R11 | K1/K2/K3 (cost caps) | primary trigger surface for autotuner cap adjustments |

**Forbidden combinations** (extends #926 §9 denylist):

- R6 → autotuner: NEVER in v1 — no K knob covers latency budgets.
- R10 → autotuner: NEVER — scheduler priority knobs widen the autonomy envelope (#926 §4.4 exclusion).
- R9 → autotuner: ONLY after MVR-3-T4 landing AND only via K4 (templates), NEVER K1/K2/K3 (cost) or K5 (banned-phrase).

**Substrate event for R7/R8/R9/R11 autotuner consumption.** Each rule's `Finding` row carries `autotuner_admissible: bool` per the §5.3 T_R11_c contract. #926 §8 decision pseudo-code reads this field as the entry gate.

---

## §9 Followups — tracking-issue candidates

Per `feedback_unaddressed_load_bearing`. File at wedge-unblock dispatch time, not at spec-merge time.

1. **`slo_alert_firing` substrate event kind.** R6's G6b suppressor (§3.1) requires an event the SLO surface does NOT currently emit. Add an Alertmanager → substrate bridge (or scrape Sloth state). One-file emitter PR.
2. **`pr_force_push_detected` distinction.** §6.3 default may over-count. If false-positive rate >20% at 30d soak, add the tree-hash compare to prwatch.
3. **MAD primitive — `internal/selfimprove/stat/mad.go`.** Shared by R6 + R7. Lands first when R6 dispatches.
4. **Cold-start sample-size YAML field.** Currently §3.1 G6a / §3.2 G7a hard-code 100 / 30. YAML-override needed for operators on fresh installs.
5. **R9 + MVR-3-T4 integration.** Wire R9 LLM-nightly proposals through the methodology-gate suite once MVR-3-T4 lands.
6. **R7+R11 co-fire predicate — sliding window.** Currently §3.6 / §5.3 T_R11_c uses a 14d window. After 30d soak, audit whether window length needs tuning per cost-cap value.
7. **R8 G8c first-day-of-feature exclusion calibration.** 60s threshold is a guess. May need adjustment.
8. **Per-rule mute CLI extension.** W4 §8.1 ships `regatta self-improvement mute <rule_name>`. Confirm R6-R11 are mute-able by name without changes.

---

## §10 Self-host filter

Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1:

| Component | Self-host need? | Verdict |
|---|---|---|
| R8 (rework-cycle) | Yes — operator hit force-push storms in observed waves | In scope (first to land) |
| R10 (priority-thrash) | Yes — operator hit re-pick loops on stuck items | In scope |
| R11 (cap-thrash) | Yes — autotuner needs the signal | In scope |
| R6 (latency-outlier) | Yes — visibility into tail-spikes BEFORE SLO budget burn | In scope |
| R7 (cost-outlier) | Yes — closes the autotuner K1/K2/K3 loop | In scope (gated on R11) |
| R9 (success-pattern-extract) | Conditionally — value depends on MVR-3-T4 | In scope post-MVR-3-T4 |
| Per-agent_id z-threshold override | No — single operator | Phase-X |
| General correlation engine | No — one named co-fire is enough | Phase-X |
| Real-time streaming detection | No — 6h cron suffices | Out of scope (W4 cadence) |

---

## §11 Deletion default

Per `feedback_deletion_default`:

- **Operator log-scanning shrinks further.** W4 closed R1-R5 patterns. R6-R11 extend coverage to outlier + cross-event patterns the operator currently hunts manually in OTel dashboards.
- **Autotuner closed-loop replaces hand-edits.** R7/R8/R11 → #926 K1-K4 means cost-cap and dispatch-template adjustments stop being weekly operator chores.
- **R9 deletes a future "why are some PRs always fast" hunt.** Once MVR-3-T4 + R9 are wired, the operator stops manually pattern-matching p10 PRs.

Surface added: one new file `internal/selfimprove/rules_r6_r11.go` (~150 LoC for 6 rules sharing the MAD primitive) + one stat helper file `internal/selfimprove/stat/mad.go` (~30 LoC) + tests. Each rule survives `feedback_default_simpler` audit (§7 out-of-scope kills speculative scaffold).

---

## §12 Cites + prior art

- `docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md` §3-§5 — parent rule shape.
- `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` §6 / §9 — closed-loop K-knob table + denylist.
- `docs/engineer/specs/2026-06-03-mvr-3-t4-research-mode-overlay-skeleton.md` — R9 methodology-gate dependency.
- `internal/selfimprove/rules.go:13-17` — R1-R5 constants + `streakRule` primitive.
- `internal/selfimprove/detector.go:34-104` — `Detector` + `EventFetcher` shape.
- `internal/orchestrator/prwatch/` — `agent_pr_head_changed` emitter (R8 source).
- `internal/orchestrator/scheduler/scheduler.go:281` — scheduler tick latency + `scheduler_picked` (R10 source).
- `internal/cost/cap/cap.go:148,151` — cap-throttled counter + spend gauge (R7, R11 source).
- `slo/*.yaml` — Sloth surface audited in §2.
- Tukey "Exploratory Data Analysis" (1977) — MAD-based outlier detection prior art (referenced, not imported).
- Kubernetes HPA `behavior.scaleUp / scaleDown` (v1.18+) — already cited by #926; same shape applies to R7+R11 → autotuner.

---

## §13 Adversarial review — design self-review

Per `feedback_adversarial_review` + `feedback_adversarial_review_every_step`:

| Tier | Finding | Disposition |
|---|---|---|
| Med | R6 vs SLOs — risk of paging fatigue if R6 files issues during a partial-SLO-burn window | Fixed inline: §2.2 + §3.1 G6b suppressor; R6 only fires when SLO is GREEN. |
| Med | R9 carries methodology risk before MVR-3-T4 — false success pattern poisons templates via #926 K4 | Fixed inline: §3.4 G9a hard-block R9 → issue until MVR-3-T4 lands; LLM proposals only. |
| Med | R8 G8a operator-author exclusion may misclassify a legitimate operator hand-fix mid-cycle as "not rework" | Accepted residual: operator hand-fixes ARE NOT a quality-feedback gap in the brief — the rework signal is agent-side. Documented; revisit at first false-negative report. |
| Low | R10 G10a progress definition (resolved = `pr_merged` OR operator-actor `pr_closed`) is the operator-decided default | Fixed inline: §6.1 counts an operator-legitimate pre-merge close as progress, not thrash, via `closed_by_actor`; default + reopen-trigger in place. |
| Low | R11 daily-bucket UTC vs operator timezone may split incidents | Documented §6.4 as decision-required; default + reopen-trigger in place. |
| Low | R7 G7d "at least 1 distinct repeat" — one-shot 5σ outlier won't fire until second occurrence | Defensible: single 5σ event may be legitimate task complexity; pattern detection requires N>1. |

No re-spawn required; spec achieves design adequacy at SKELETON tier. Material elaboration (impl-ready) waits on wedge unblock.

---

## §14 Comment sweep

This spec is prose only — no inline source comments. Comment-sweep state: **clean**.

---

```release-notes
none (internal design spec — skeleton-prefetch tier per #832 wedge deferral)
```

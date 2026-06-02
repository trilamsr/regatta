---
id: OBS-WAVE-A-T5
title: SLO-1 + SLO-2 OpenSLO YAML + Sloth compile + runbooks + slo dashboard
lane: observability
status: planned
dependencies: OBS-WAVE-A-T1, OBS-WAVE-A-T2, OBS-WAVE-A-T3
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §5 SLO-1 + SLO-2, §7 Wave-A table row A-T5, §9 R3 (Sloth version pin), §10 dispatch brief A-T5.
Amendment ref: review of PR #410 §5 L4 — SLO-3 (PR-merge-rate) demoted to KPI tile; renumber treats this PR's two SLOs as SLO-1 + SLO-2 only. SLO-2 widen + quantile rewrite explicitly deferred as `[OBS-followup] #1` (trigger: 30 days of real burn-rate from Wave-B).

## Task

Write two OpenSLO YAML specs at:

- `slo/scheduler-tick.yaml` — SLO-1: scheduler tick p95 ≤ 5s, 7d window, 5% error budget, multi-burn-rate alerts (14.4× fast, 6× slow per Google SRE workbook). SLI: `histogram_quantile(0.95, rate(regatta_scheduler_tick_latency_ms_bucket[5m])) <= 5000`.
- `slo/l4-latency.yaml` — SLO-2: L4 gate p95 ≤ 30s, 7d window, 1% error budget, multi-burn-rate alerts. SLI: `histogram_quantile(0.95, rate(regatta_l4_latency_ms_bucket[5m]))`.

Pin Sloth version at `tools/sloth/version` (spec §9 R3 mitigation). Add Make target `make slo-compile` that:

1. Reads every `slo/*.yaml`.
2. Invokes the pinned Sloth binary.
3. Writes Prom recording + alert rules to `dashboards/prometheus/rules/`.
4. Is deterministic — same input YAML produces byte-equal output rules.

Runbooks (one per SLO) at:

- `docs/operator/runbooks/scheduler-tick.md` — what fires, what to check first (gate_l4 vs persist step), how to acknowledge, escalation tier.
- `docs/operator/runbooks/l4-latency.md` — what fires, common causes (cache cold, second-opinion fan-out, model API slowdown), how to bisect by `category` label.

Each runbook is ≤ 200 lines, falsifiable steps only — no banned phrases per `feedback_doc_check_banned_phrases` (pre-push grep mandatory). Per `feedback_decision_priority` UX > velocity: every step answers "what does the operator do in the next 60 seconds?"

Grafana panel `dashboards/grafana/slo.json` shows SLO burn-rate over time for both SLOs (one panel per SLO, plus a combined "error-budget-remaining" stat tile per SLO).

**Renumber + deferral (amendment §5 L4):**

- Old SLO-3 (PR-merge-rate) demoted to KPI tile — no `slo/*.yaml` for it. KPI tile lives on a Wave-D dashboard, not here.
- Old SLO-4 (substrate event-rate) demoted critical→warn — ships in Wave B (B-T1), not here.
- SLO-2 budget tightness + quantile rewrite deferred. File `[OBS-followup] #1 — SLO-2 widen + quantile rewrite after 30 days of Wave-B burn-rate data` at A-T5 merge per `feedback_unaddressed_load_bearing`.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean + `make slo-compile` exits 0; both YAML files parse cleanly; runbooks + dashboard JSON exist; B1+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer subagent clears; A1 + A2 + A3 (runbook-per-critical-alarm) from spec §8; `[OBS-followup] #1` tracking issue filed at merge per `feedback_unaddressed_load_bearing`; banned-phrase grep clean on runbooks.
- **A+ (stretch):** A + `TestSLOBurnRate_FiresOnSyntheticBreach` exit 0 (A+2 from spec §8) — synthetic load injector verifies fast-burn alert fires; `make slo-compile` byte-deterministic (`TestSlothCompile_Deterministic`); `tools/sloth/version` pin file exists + matches the binary.

## Acceptance criteria

- [planned] c1: `slo/scheduler-tick.yaml` (SLO-1) + `slo/l4-latency.yaml` (SLO-2) check in with multi-burn-rate alerts per spec §5.
- [planned] c2: `make slo-compile` Make target invokes pinned Sloth; outputs to `dashboards/prometheus/rules/`; `tools/sloth/version` pinned (spec §9 R3).
- [planned] c3: Runbooks at `docs/operator/runbooks/scheduler-tick.md` + `docs/operator/runbooks/l4-latency.md`; banned-phrase grep clean (`feedback_doc_check_banned_phrases`).
- [planned] c4: `dashboards/grafana/slo.json` with burn-rate panels checked in; PromQL refs resolve to emitted metric names (spec §9 R2).
- [planned] c5: `[OBS-followup] #1` tracking issue filed at merge for SLO-2 widen + quantile rewrite (amendment §5 L4 deferral).
- [planned] c6: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.

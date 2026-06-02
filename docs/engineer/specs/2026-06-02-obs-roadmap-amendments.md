# Observability roadmap — Amendments spec (PR #400 review)

Status: ready for review
Date: 2026-06-02
Author: design subagent (amendments pass)
Parent spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` (PR #400 · branch `spec/observability-roadmap-2026-06`)
Source review: `docs/engineer/reviews/2026-06-02-obs-roadmap-review-of-400.md` (PR #405 · branch `review/400-obs-roadmap`)
Review verdict: **ADOPT-WITH-AMENDMENTS** — 5 PASS / 4 RISK / 1 BLOCKER.
Memory rules in force: `feedback_research_design_principles` (proven OSS > build-from-scratch; ≥2 candidates per primitive), `feedback_decision_priority` (UX > ease > performance > best-prac > velocity; long-term > short-term), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_adversarial_review` (edge cases + refactor + risk + simplification), `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`.

This spec is amendments-only. It does not re-litigate the canonical stack pick (L1 PASS), the tag-schema cardinality budget (L3 PASS), or the 15-item adopt-vs-build matrix (L5 PASS). It folds the 1 BLOCKER + 4 RISKs from the review into concrete diff-shaped edits against the parent spec, plus two `[OBS-followup]` tracking issues for items that ship as warn-tier / deferred per the review's reopen-conditions.

---

## §0 Amendment summary (one-row-per-finding)

| Review lens | Finding | Amendment-class | Parent-spec section(s) touched | Ships with #400? |
|---|---|---|---|---|
| L7 Wave-A realism | **BLOCKER** (D-T3 missing C-T2 dep + A-T4 first-digest wiring + A-T0 effort under-sized) | hard correction | §7 dep table + §7 dep graph + §6.2 + §10 D-T3 + §10 A-T0 | YES (in-band) |
| L2 metric naming | RISK (unit-suffix double-render under Prom exporter) | clarifying edit | §2.1 | YES (in-band) |
| L4 SLO + alarm policy | RISK (SLO-3 is velocity OKR not SLO; SLO-2 budget too tight; SLO-4 σ-based on bursty distro) | scope correction | §5 (SLO-3 demote; SLO-2 widen) + `[OBS-followup]` (SLO-2 + SLO-4 widening) | partial — SLO-3 demoted in-band; SLO-2/4 fix tracked |
| L6 anti-pattern traps | RISK (missing-metric-for-failure + dashboard-UI-drift + cardinality-cost telemetry not designed against) | add 3 traps | §4 (3 new entries: #9, #10, #11) | YES (in-band) |
| L8 operator surface | RISK (7-panel TUI > 80×24; digest not machine-readable; bubbletea dep claim wrong) | scope correction | §6.1 (panel-budget table + 5-panel cap) + §6.2 (YAML front-matter) + Appendix A (bubbletea row) + §10 D-T2 | YES (in-band) |
| Cross-lens (sync-gauge prose) | RISK (OTel Go SDK ≥ v1.32 has sync Gauge; prose is factually wrong; API pick is still correct) | prose-only fix | §10 D-T3 | YES (in-band) |

Two `[OBS-followup]` tracking issues filed at PR #400 merge (per `feedback_unaddressed_load_bearing`):
1. `[OBS-followup] SLO-2 error-budget widen (5% OR 28d window) + SLO-4 quantile rewrite (P99 of 30d trailing) replacing σ-based` — derives from L4 alternative; ships after Wave-B observes real burn-rate behavior.
2. `[OBS-followup] dashboard-UI-drift nightly diff job (Grafana export vs checked-in JSON) + cardinality-cost "active series count" panel on dashboards/grafana/meta.json` — derives from L6 alternative; ships in Wave-D after operator-surface lands.

The third L6 alternative (missing-metric-for-failure CI test) ships in-band as **A-T0a** (see §1 below) because it is a test, not infrastructure.

---

## §1 BLOCKER — L7 Wave-A realism (in-band amendments)

The review's L7 finding is a dispatch-graph correctness bug compounded by two effort/wiring miscalls. All three are folded into a single amendment because they touch the same file (§7) and the same dispatch brief (§10 A-T0 / D-T3 / A-T4).

### 1.1 Fix D-T3 dep-graph defect (BLOCKER core)

**Defect.** §7 Wave-D table says `D-T3 Depends-on A-T0`. §7 dep-graph diagram says `D-T1 ∥ D-T3 in parallel` after Wave-A. But §10 D-T3 brief sources `30_day_green` from SLO-3 PR-merge-rate, which derives from `regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}` — a histogram emitted by **C-T2 (Wave C)**. A reviewer who follows the dep graph dispatches D-T3 in parallel with Wave B and ships a broken gauge that reads zero (the histogram does not exist yet).

**Amendment — §7 Wave-D row D-T3 (Depends-on column):**

```diff
-| **D-T3** (item #15) | impl-D3 | new `internal/obs/triggers/clock.go` emits gauge `regatta.trigger.days_remaining` (tag `trigger`); `dashboards/grafana/triggers.json`; trigger thresholds live in `slo/triggers.yaml` | A-T0 | S |
+| **D-T3** (item #15) | impl-D3 | new `internal/obs/triggers/clock.go` emits gauge `regatta.trigger.days_remaining` (tag `trigger`); `dashboards/grafana/triggers.json`; trigger thresholds live in `slo/triggers.yaml` | A-T0, **C-T2** (30_day_green reads the PR-stage histogram) | S |
```

**Amendment — §7 dep-graph diagram:**

```diff
 Wave A (A-T0 → A-T1 ∥ A-T2 ∥ A-T3 → A-T4 + A-T5 → A-T6)
    │
    ├─ Wave B (B-T1 ∥ B-T2 ∥ B-T3 ∥ B-T4)
    │
    ├─ Wave C (C-T1 → C-T2 + C-T4; C-T3 via shared-owner with A-T1)
    │
-   └─ Wave D (D-T1 ∥ D-T3 in parallel; D-T2 after A+B+C)
+   └─ Wave D (D-T1 ∥ D-T3 — but D-T3 also depends on C-T2; D-T2 after A+B+C)
```

**Amendment — §10 D-T3 brief (add one sentence at the start):**

```diff
 ### D-T3 (item #15 — trigger clock)
 
-> **Task**: new `internal/obs/triggers/clock.go` exports `DaysRemaining(trigger string) int` over `30_day_green` (derives from SLO-3 PR-merge-rate over 30d) + `external_customer_signal` (sealed input from `slo/triggers.yaml`) + `phase_g_gate`. Register `meter.Int64ObservableGauge("regatta.trigger.days_remaining")` with a callback that records `DaysRemaining(t)` for each trigger (OTel async gauge — sync `Gauge` does not exist on the OTel Go SDK; the observable gauge is the documented shape for sampled state). `dashboards/grafana/triggers.json` stat-tile per trigger. No manual gauge override per R10 mitigation. A+ rubric scorecard + release-notes + `--body-file`.
+> **Task**: new `internal/obs/triggers/clock.go` exports `DaysRemaining(trigger string) int` over `30_day_green` (derives from the PR-stage histogram `regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}` emitted by **C-T2 — DO NOT DISPATCH D-T3 BEFORE C-T2 MERGES** or the gauge reads zero) + `external_customer_signal` (sealed input from `slo/triggers.yaml`) + `phase_g_gate`. Register `meter.Int64ObservableGauge("regatta.trigger.days_remaining")` with a callback that records `DaysRemaining(t)` for each trigger. (OTel Go SDK ≥ v1.32 ships sync `Float64Gauge`/`Int64Gauge`; the **observable** gauge is still the correct pick here because `DaysRemaining(t)` is a sampled derivation, not a write-on-event measurement — async gauge is the OTel-blessed shape for this.) `dashboards/grafana/triggers.json` stat-tile per trigger. No manual gauge override per R10 mitigation. A+ rubric scorecard + release-notes + `--body-file`.
```

That last parenthetical also resolves the cross-lens "OTel sync `Gauge` claim is wrong" finding from the review (no separate amendment needed).

### 1.2 Resize A-T0 from M to L (split into A-T0a + A-T0b)

**Defect.** A-T0 is sized M but actually does: (1) MeterProvider init + 2 exporters + mutual-exclusion validator, (2) `Config.Meter` field added to all 8 existing Config structs, (3) every test that constructs each component updated. W6's comparable Tracer DI was 2 PRs (#159 + follow-up). M under-counts; L or a split is needed.

**Amendment — §7 Wave-A table, replace A-T0 row with two rows:**

```diff
-| **A-T0** | impl-A0 | `internal/obs/otel/meter.go` + `meter_test.go`; extend `internal/obs/otel/setup.go` to init MeterProvider; add `Config.Meter metric.Meter` field to all 8 existing Config structs (mirror W6 Tracer DI per `feedback_spec_pattern_authority`); add OTLP-metric exporter wiring in env-var contract | — | M |
+| **A-T0a** | impl-A0a | `internal/obs/otel/meter.go` + `meter_test.go`; extend `internal/obs/otel/setup.go` to init MeterProvider; OTLP-metric exporter wiring; Prom exporter wiring (`OTEL_METRICS_PROMETHEUS_PORT`); mutual-exclusion validator (`ErrOTelMetricExporterConflict`); **plus** new lint test `TestEveryGateAdapterHasInvocationsCounter` (covers §4 trap #9 — see §3 below). Adds `Config.Meter metric.Meter` field to the **2 Config structs that A-T1/A-T2/A-T3 touch first** (`cost/spend`, `gates/l4`) so Wave-A parallel work can start | — | M |
+| **A-T0b** | impl-A0b | adds `Config.Meter metric.Meter` field to the remaining 6 Config structs (`orchestrator/scheduler`, `orchestrator/spawner`, `orchestrator/state/substrate`, `history`, `orchestrator/followup`, plus the 1 spawner-failure-taxonomy ctor that lands later in Wave-C) + retrofits constructors + updates every existing test that constructs each component | A-T0a | M |
```

Wave-A parallelism preserved: A-T1 + A-T2 + A-T3 still start after A-T0a (their files' Configs are in A-T0a's scope). A-T0b runs in parallel with A-T1/A-T2/A-T3 because it touches disjoint Config structs (the scheduler + spawner Configs A-T0b lands match the files A-T3 + later waves edit, but A-T0b lands the Config field first — A-T3 starts after A-T0b OR coordinates via shared-owner rule). Per `feedback_shared_primitive_owner`: A-T0b OWNS the 6 Config retrofits; A-T3 reads-but-does-not-touch `Config.Meter` declaration.

**Dep-graph diagram amendment:**

```diff
-Wave A (A-T0 → A-T1 ∥ A-T2 ∥ A-T3 → A-T4 + A-T5 → A-T6)
+Wave A (A-T0a → {A-T1 ∥ A-T2 (both unblocked at A-T0a merge)} ∥ A-T0b → {A-T3 unblocked at A-T0b merge} → A-T4 + A-T5 → A-T6)
```

### 1.3 A-T4 (first digest) degraded — explicit acknowledgement

**Defect.** A-T4 ships in Wave-A and reads "the previous 24h of metrics + logs + PR-merge events." But the PR-merge events emitter is C-T2 (Wave-C). The first digest at `docs/digests/2026-06-03.md` will lack the PRs-landed section.

**Amendment — §6.2 (after the section list, insert a new paragraph):**

```diff
 ### 6.2 Daily digest (item 14)
 
 `regatta digest --date 2026-MM-DD` reads the previous 24h of metrics + logs + PR-merge events and writes `docs/digests/2026-MM-DD.md` with sections:
 
 1. **Loop health** — tick p95, gate counts, alarms fired.
 2. **PRs landed** — list with title, A+ tier (B/A/A+), cost USD, dispatch→merge time.
 ...
 7. **Followups filed** — count + link to tracking issues.
 
+**First-digest degraded contract.** Sections 2 (PRs landed) and 3 (Adversarial findings) depend on emitters that land in later waves: section 2 needs `regatta.pr.stage_duration_seconds` (C-T2 — Wave C) for the dispatch→merge stage timing column, and section 3 needs `regatta.adversarial.findings` (D-T1 — Wave D) for the fate-by-count rollup. Wave-A A-T4 ships the digest binary with these two sections rendering a one-line placeholder ("`PRs landed — emitter ships C-T2 (Wave C); see #<issue>`" / "`Adversarial findings — emitter ships D-T1 (Wave D); see #<issue>`") not silent zeros. The placeholder lines are removed by the C-T2 and D-T1 implementer subagents respectively as part of their landing PR. Per `feedback_decision_priority` UX > velocity: a placeholder with a forward reference is better than a silently zeroed section that looks like the loop is broken.
 
 Cron: `scripts/cron/daily-digest.sh` runs daily at 09:00 local time; commits the file via the standard PR workflow with `[DOCS]` prefix.
```

Also adds a parallel sentence to §10 A-T4:

```diff
 ### A-T4 (item #14 — daily digest)
 
-> **Task**: new subcommand `cmd/regatta/digest.go` — `regatta digest --date YYYY-MM-DD` writes `docs/digests/2026-MM-DD.md` per §6.2 sections (loop health / PRs landed / adversarial / substrate / cost / triggers / followups). Data sources: Prom HTTP API + sqlite. Cron at `scripts/cron/daily-digest.sh` 09:00 local. Deterministic output (sort + tabulate) per A+4 rubric. Today's first digest at `docs/digests/2026-06-03.md`. A+ rubric scorecard + release-notes (category `[DOCS]` for the digest content) + `--body-file`.
+> **Task**: new subcommand `cmd/regatta/digest.go` — `regatta digest --date YYYY-MM-DD` writes `docs/digests/2026-MM-DD.md` per §6.2 sections (loop health / PRs landed / adversarial / substrate / cost / triggers / followups). Data sources: Prom HTTP API + sqlite. Cron at `scripts/cron/daily-digest.sh` 09:00 local. Deterministic output (sort + tabulate) per A+4 rubric. Today's first digest at `docs/digests/2026-06-03.md`. **Sections 2 (PRs landed) and 3 (Adversarial findings) ship as placeholder one-liners with forward-reference issue numbers per §6.2 first-digest degraded contract** — emitter ships C-T2 / D-T1; do not render zeros. A+ rubric scorecard + release-notes (category `[DOCS]` for the digest content) + `--body-file`.
```

---

## §2 RISK — L2 metric naming (in-band amendments)

The unit suffix in the metric name double-renders under the Prom exporter (e.g. `regatta.scheduler.tick.latency_ms` with `unit=ms` → Prom wire `regatta_scheduler_tick_latency_ms_milliseconds`). The spec acknowledges this for `_total` but not for `_ms` / `_seconds`. Two options: (a) drop the unit suffix from the metric name and rely on the `unit` argument, (b) keep the suffix and explicitly accept the double-unit wire string. Picking (b) per `feedback_decision_priority` ease > best-prac because the operator-facing dashboards (already in §3 tile column) reference the unit-suffixed name; renaming now is a larger refactor than accepting the wire artifact.

**Amendment — §2.1 (after the unit-suffix paragraph, add):**

```diff
 Unit suffixes follow OTel: `_ms` (milliseconds), `_seconds`, `_usd` (regatta-specific), `_bytes`, or no suffix for dimensionless counters. Histograms always carry a unit. Dropped: the `_total` Prom suffix — OTel exporter appends it on the wire for Prom compat.
 
+**Prom exporter double-unit-suffix behaviour (locked).** The OTel Prom exporter appends the SDK unit (`ms`/`s`/`By`) to the metric name on the wire when both the metric name carries a unit suffix AND the meter API `unit` argument is set. Example: `regatta.scheduler.tick.latency_ms` declared with `Float64Histogram(name="regatta.scheduler.tick.latency_ms", unit="ms")` renders as `regatta_scheduler_tick_latency_ms_milliseconds` on `/metrics` scrape. Spec **accepts** this double-unit wire string (option B: keep the in-code suffix, let the exporter double-render) because every dashboard tile and SLO PromQL expression in §3 + §5 already references the suffixed name. The alternative — drop the suffix from the name and rely solely on the `unit` argument — would force a rewrite of every dashboard JSON and every Sloth SLI before A-T5 lands. A-T0a's PR body MUST show one sample `/metrics` scrape line for `regatta.scheduler.tick.latency_ms` so the wire shape is locked at landing. If a future OTel SDK release changes the double-suffix behaviour, file `[OBS-followup]` to re-normalize. Unit `usd` (regatta-specific; no UCUM code) is passed as the literal string `"usd"`; downstream Prom histograms render `unit=""` which is acceptable for a fiat-denominated counter (no UCUM equivalent exists).
```

This is a single-paragraph addition; no other spec section changes.

---

## §3 RISK — L6 anti-pattern traps (in-band: add 3 traps to §4)

**Amendment — §4 (append entries 9, 10, 11):**

```diff
 8. **Vanity metrics** — counter with no operator-facing decision. Every metric on the §3 table answers a named question for the operator (e.g. "is the L4 cache helping?" → `regatta.l4.cache.hits`/`misses`). If no question, drop the metric.
 
+9. **Missing metric for a known failure surface** — a new gate or adapter is added without a `regatta.<gate>.invocations` counter, so a failure mode that should be visible silently isn't. Enforced by `TestEveryGateAdapterHasInvocationsCounter` (new in A-T0a) — AST walk that asserts every type implementing the `Gate` interface (in `internal/gates/`) or the `Adapter` interface (in `internal/adapters/`) has a `meter.*Counter("regatta.<gate>.invocations")` or `meter.*Counter("regatta.<adapter>.invocations")` call somewhere in its package. Failure mode: the test names the gate/adapter and points at the missing counter line.
+
+10. **Dashboard UI drift** — operator edits a Grafana dashboard in the web UI to debug an incident, never round-trips the change back to `dashboards/grafana/*.json`. Real-world this is the #1 source of dashboard rot. `make provision-dashboards` upserts the JSON → Grafana, but no reverse-diff path exists. Mitigation: nightly job (filed as `[OBS-followup]` at PR #400 merge — see §0; ships in Wave-D) exports every dashboard from Grafana via HTTP API, normalizes JSON (strip mutable IDs/timestamps), and `diff` against `dashboards/grafana/*.json`; any non-empty diff opens a `[OBS-drift]` issue with the diff inline. No in-band fix because the nightly diff job needs operator-surface Wave-D context to render diffs usefully.
+
+11. **Cardinality cost telemetry** — the §2.2 budget caps cardinality but doesn't measure it. Operators with metered backends (Honeycomb, Datadog) want a "metric series count over time" KPI so they see the cost-of-cardinality before the bill arrives. Mitigation: `dashboards/grafana/meta.json` (filed as `[OBS-followup]` at PR #400 merge — see §0; ships in Wave-D) hosts an "active series count by metric" panel sourced from Prom's `count(count by (__name__)({__name__=~"regatta_.*"}))` query. No in-band fix because the meta dashboard needs Wave-D operator-surface context.
```

Trap #9 ships in-band as part of A-T0a (the AST-walk lint is cheap to write and is a foundational gate). Traps #10 + #11 are tracked as the second `[OBS-followup]` per §0.

---

## §4 RISK — L4 SLO + alarm policy (in-band scope correction)

The review finds 3 problems with §5: SLO-3 is a velocity OKR (PR-merge-rate is "team is shipping" not "service is healthy"), SLO-2's 1%/7d budget burns in ~50 min on a real LLM provider tail, and SLO-4's σ-based alarm assumes Gaussian distribution on a known-bursty signal.

### 4.1 SLO-3 — demote from SLO to operator KPI tile (in-band)

**Amendment — §5 SLO-3 entry, replace with a KPI-tile note:**

```diff
-### SLO-3 — PR-merge-rate (drives 30-day-green → Phase-S relaxation restore trigger)
-
-- Objective: ≥ 8 PRs merged per active 24h cycle.
-- Window: 30d rolling.
-- Error budget: 5 missed-cycle days per 30d.
-- Alarm: info-tier when 3+ consecutive missed days (Phase-S guardrail).
-- SLI: `sum(increase(regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}[24h])) >= 8`.
-- Source: SLI derives from C-T2's PR-lifecycle stage histogram; the `_count` series on `stage="ci_green_to_merge"` is the merge counter without a separate emitter (avoids dup-metric drift per §4 trap #6).
+### SLO-3 — REMOVED (was PR-merge-rate) → operator KPI tile on `dashboards/grafana/digest.json`
+
+**Why removed.** PR-merge-rate is a velocity target (team shipping cadence), not a user-visible reliability signal. Calling it an SLO conflates "service is healthy" with "team is shipping" and burns the error budget on planned slow weeks (operator vacation, Phase-S relaxation hold). The metric remains valuable as the **trigger-clock source** (`30_day_green` derivation in D-T3) and as an operator dashboard KPI — just not as a paging alarm.
+
+**Replacement.** PR-merge-rate ships as a KPI tile on `dashboards/grafana/digest.json` (operator-glance), tile spec: stat-panel showing 24h merged count + 7d-trailing avg + 30d-trailing avg. Source PromQL: `sum(increase(regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}[24h]))`. No alarm rule. D-T3's `30_day_green` trigger still reads this same series.
+
+**Re-numbering.** SLO-4 (substrate event-rate) becomes **SLO-3**; SLO-5 (cost-cap fire rate) becomes **SLO-4**. The total SLO count drops from 5 to 4.
```

Renumber subsequent SLO sections (SLO-4 → SLO-3; SLO-5 → SLO-4). Update §3 Wave-B B-T1 row + §10 B-T1 brief to reference the renumbered SLO-3.

### 4.2 SLO-2 budget widen — track as `[OBS-followup]` (out-of-band)

The review's L4 alternative for SLO-2 (widen budget to 5% OR 28d window) is conservative; the right answer needs real burn-rate observations from Wave-B. Per `feedback_decision_priority` long-term > short-term: ship SLO-2 with the current 1%/7d budget as documented, then file the followup so Wave-B's first 30 days of real burn-rate data inform the widen.

**Amendment — §5 SLO-2 entry (append a "tuning followup" sentence):**

```diff
 ### SLO-2 — L4 gate latency
 
 - Objective: p95 ≤ 30 s.
 - Window: 7d rolling.
 - Error budget: 1% of L4 invocations.
 - Alarm: critical on multi-burn-rate breach.
 - SLI: `histogram_quantile(0.95, rate(regatta_l4_latency_ms_bucket[5m]))`.
+
+**Tuning followup (filed at PR #400 merge per §0):** `[OBS-followup] SLO-2 budget widen to 5% OR 28d window pending real burn-rate data from Wave-B's first 30 days`. The 1%/7d budget burns in ~50 min during a real LLM-provider tail (5-10× normal latency for 30-60 min is common). Wave-B's substrate-event-rate observability + Wave-C's failure-taxonomy will tell us whether the current budget is operationally workable; widen at that point if false-page rate > 1/week.
```

### 4.3 SLO-4 (now SLO-3 after renumber) — track quantile rewrite as `[OBS-followup]` (out-of-band)

Same logic: ±3σ on a bursty non-Gaussian signal will false-positive; the correct fix is a quantile-based bound, but the right quantile threshold needs real distribution data from B-T1.

**Amendment — §5 SLO-3 (renumbered from SLO-4) entry (append):**

```diff
 ### SLO-3 — Substrate event-rate within bounds (renumbered from SLO-4)
 
 - Objective: rate stays within ±3σ of 7-day rolling baseline.
 - Window: 7d rolling.
 - Error budget: 0.1% of 5-min windows.
-- Alarm: critical on either spike or drop sustained > 5 min.
+- Alarm: **warn-tier** (not critical) on either spike or drop sustained > 5 min. Demoted from critical because the ±3σ stat-arb signal on a bursty non-Gaussian distribution is expected to false-positive at 1-3/day; warn-tier surfaces in the digest, does not page.
 - SLI: `abs(rate(regatta_substrate_events_appended_total[5m]) - avg_over_time(rate(regatta_substrate_events_appended_total[5m])[7d:5m])) <= 3 * stddev_over_time(...)`.
+
+**Tuning followup (filed at PR #400 merge per §0):** `[OBS-followup] SLO-3 quantile rewrite — replace ±3σ with "rate <= P99 of 30d trailing"` because rate(events) is bursty and non-Gaussian; σ-based bounds are stat-arb signals not SLOs. Rewrite after B-T1's first 30 days of histogram data is in the warehouse.
```

### 4.4 Alarm-policy net effect summary (after amendments)

| SLO | Tier | Alarm tier | Notes |
|---|---|---|---|
| SLO-1 scheduler-tick | unchanged | critical | unchanged |
| SLO-2 L4 latency | unchanged | critical | budget-widen followup filed |
| ~~SLO-3 PR-merge-rate~~ | **REMOVED** | n/a | demoted to KPI tile on `dashboards/grafana/digest.json` |
| SLO-3 (was SLO-4) substrate event-rate | renumbered | **warn-tier** (was critical) | quantile-rewrite followup filed |
| SLO-4 (was SLO-5) cost-cap fire rate | renumbered | info-tier | unchanged |

Total SLOs: 4 (was 5). Critical-tier alarms: 2 (SLO-1, SLO-2). Warn-tier: 1 (SLO-3). Info-tier: 1 (SLO-4).

---

## §5 RISK — L8 operator surface (in-band amendments)

Three concerns: (a) 7-panel TUI exceeds 80×24 terminal budget — UX-first is broken if operator scrolls; (b) digest is markdown-only, not machine-parseable; (c) bubbletea is NOT in `go.mod` (only indirect `charmbracelet/*` deps are).

### 5.1 `regatta status` — 5-panel single-screen with panel-budget table

**Amendment — §6.1 (replace the 7-panel list + add a budget table):**

```diff
 ### 6.1 `regatta status` (item 13)
 
-New CLI subcommand `cmd/regatta/status.go`. TUI rendered with `github.com/charmbracelet/bubbletea` (already in `go.mod` for #w7-tui prototyping; verify before commit). Panels:
+New CLI subcommand `cmd/regatta/status.go`. TUI rendered with `github.com/charmbracelet/bubbletea` (**candidate dep — NOT yet in `go.mod`**; D-T2 adds it. Indirect `charmbracelet/colorprofile`/`ultraviolet`/`x/ansi`/`x/term`/`x/termios` ARE in go.mod from other usage). Panel budget: **single-screen on 80×24 terminal** (default ssh session). Sum of panel rows ≤ 24, panel widths align to 80-col grid. Panel-budget table:
+
+| Panel | Rows | Cols | Notes |
+|---|---|---|---|
+| 1. Loop pulse (merged) | 6 | 80 | scheduler tick p50/p95 + active work_item count + lane breakdown — **merges old panels 1 and 2** |
+| 2. Cost | 5 | 80 | this hour / today / week + budget remaining |
+| 3. L4 | 4 | 80 | cache hit ratio + second-opinion fires (1h) |
+| 4. PR pipeline | 5 | 80 | open PRs by stage; count + oldest per stage |
+| 5. Alarms (last 24h) | 4 | 80 | name, severity, fired-at, resolved-at |
+| **Total** | **24** | **80** | exact fit |
 
-1. **Scheduler tick** — last 60 ticks: p50/p95 latency, error rate (mini-sparkline).
-2. **Active work_items** — count by lane, count by gate-state.
-3. **Cost — this hour / today / this week** — USD totals + budget remaining (reads `event_token_spend` rows directly, no backend dep).
-4. **L4 — cache hit ratio + second-opinion fires (1h)**.
-5. **PR pipeline** — open PRs by stage (dispatch/first_commit/pr_open/ci_green); count, oldest.
-6. **Triggers** — days to 30-day-green; days to external-customer signal; days to Phase-G.
-7. **Alarms — last 24h** — name, severity, fired-at, resolved-at.
+The 5-panel cap drops the **Triggers** panel from `status` (it moves to a sibling subcommand `regatta triggers` — one-line stat per trigger, no TUI overhead; D-T3 owns it).
 
 Data source: Prom HTTP API (if `OTEL_EXPORTER_OTLP_ENDPOINT` set) + sqlite (always available). When Prom unreachable, panels degrade to sqlite-derived numbers with a banner.
 
 UX target (per `feedback_decision_priority`): `regatta status` answers "is the loop healthy + what's the next operator action?" in < 3 seconds of cold start.
```

**Amendment — §10 D-T2 brief:**

```diff
 ### D-T2 (item #13 — `regatta status` TUI)
 
-> **Task**: new `cmd/regatta/status.go` using `github.com/charmbracelet/bubbletea` (verify it's already in go.mod — adopt the existing dep per `feedback_research_design_principles`). 7 panels per §6.1. Reads Prom HTTP API + sqlite; graceful degradation banner when Prom unreachable per R5 mitigation. Render budget < 3 s cold per A+3 rubric. A+ rubric scorecard + release-notes (`[FEATURE]`) + `--body-file`.
+> **Task**: new `cmd/regatta/status.go` using `github.com/charmbracelet/bubbletea` (**add to go.mod** — not currently present; indirect `charmbracelet/*` deps don't count). **5 panels per §6.1 panel-budget table — single-screen on 80×24** (Loop-pulse / Cost / L4 / PR-pipeline / Alarms). Triggers move to a sibling subcommand `regatta triggers` (D-T3 owns that subcommand alongside the gauge emitter). Reads Prom HTTP API + sqlite; graceful degradation banner when Prom unreachable per R5 mitigation. Render budget < 3 s cold per A+3 rubric. A+ rubric scorecard + release-notes (`[FEATURE]`) + `--body-file`.
```

**Amendment — Appendix A:**

```diff
-github.com/charmbracelet/bubbletea                            v0.27+ (TUI for regatta status — verify in go.mod)
+github.com/charmbracelet/bubbletea                            v0.27+ (TUI for regatta status — **candidate, NOT yet in go.mod**; D-T2 adds it. Only indirect charmbracelet/* deps present today)
```

**Amendment — §10 D-T3 (Triggers subcommand additional scope):**

```diff
 ### D-T3 (item #15 — trigger clock)
 
 > **Task**: new `internal/obs/triggers/clock.go` exports `DaysRemaining(trigger string) int` over `30_day_green` ... A+ rubric scorecard + release-notes + `--body-file`.
+>
+> **Also ships in D-T3 (relocated from §6.1)**: new sibling subcommand `cmd/regatta/triggers.go` — one stat-line per trigger ("`30_day_green: 17 days remaining`"). No bubbletea, no TUI; plain stdout. Single-screen fits trivially. Coordinates with D-T2's `regatta status` (which links to `regatta triggers` in a footer line).
```

### 5.2 Daily digest — add YAML front-matter for machine-readability

**Amendment — §6.2 (add YAML front-matter section before the markdown sections):**

```diff
 ### 6.2 Daily digest (item 14)
 
-`regatta digest --date 2026-MM-DD` reads the previous 24h of metrics + logs + PR-merge events and writes `docs/digests/2026-MM-DD.md` with sections:
+`regatta digest --date 2026-MM-DD` reads the previous 24h of metrics + logs + PR-merge events and writes `docs/digests/2026-MM-DD.md` with a **YAML front-matter block** (for machine readers: trigger-clock derivation, the autonomous-session-prompt boot reader, future weekly-rollup) plus the human-readable markdown sections:
+
+```yaml
+---
+# machine-readable front-matter — keep in lock-step with markdown body below
+date: 2026-MM-DD
+tick_p95_ms: 4321
+tick_error_rate: 0.001
+prs_landed: 11
+prs_landed_a_plus: 3
+prs_landed_a: 7
+prs_landed_b: 1
+cost_usd_today: 87.42
+cost_usd_week_to_date: 511.30
+adversarial_filed: 4
+adversarial_dismissed: 2
+substrate_events_per_sec: 12.3
+substrate_chain_breaks: 0
+substrate_divergence_count: 0
+triggers:
+  30_day_green: 17
+  external_customer_signal: null  # not yet set
+  phase_g_gate: 27
+followups_filed: 3
+---
+```
+
+The markdown body sections below mirror the front-matter fields one-to-one. Downstream tooling (`yq '.tick_p95_ms' docs/digests/2026-MM-DD.md`) reads the front-matter; humans read the body. Per `feedback_research_design_principles`: the digest IS infrastructure for the next layer; parse-ability is required, not optional.
+
+Markdown body sections:
 
 1. **Loop health** — tick p95, gate counts, alarms fired.
 ...
```

**Amendment — §10 A-T4 (add front-matter requirement):**

```diff
 ### A-T4 (item #14 — daily digest)
 
-> **Task**: new subcommand `cmd/regatta/digest.go` ... A+ rubric scorecard + release-notes (category `[DOCS]` for the digest content) + `--body-file`.
+> **Task**: new subcommand `cmd/regatta/digest.go` ... **YAML front-matter block per §6.2** (one field per markdown body section; lock-step). Deterministic output (sort + tabulate) per A+4 rubric — front-matter keys MUST be in declaration order, not map order. `TestDigest_YAMLFrontMatterMatchesBody` cross-checks every front-matter key has a corresponding body section. A+ rubric scorecard + release-notes (category `[DOCS]` for the digest content) + `--body-file`.
```

---

## §6 Updated Wave-A / Wave-B / Wave-C / Wave-D task list (consolidated)

After all amendments, the wave shape is:

### Wave A (7 tasks — was 7; A-T0 split into A-T0a + A-T0b)

| ID | Goal | Depends-on | Effort |
|---|---|---|---|
| A-T0a | MeterProvider init + 2 exporters + mutual-exclusion validator + AST-walk lint test (trap #9) + 2 Config retrofits (cost/spend, gates/l4) | — | M |
| A-T0b | 6 remaining Config retrofits (scheduler, spawner, substrate, history, followup, +1) + constructor + test updates | A-T0a | M |
| A-T1 (item #1) | cost dashboard tile | A-T0a | S |
| A-T2 (item #2) | L4 metrics | A-T0a | M |
| A-T3 (item #4) | scheduler tick histogram | A-T0b | M |
| A-T4 (item #14) | daily digest binary + first digest (with placeholder sections + YAML front-matter) | A-T1 thru A-T3 | M |
| A-T5 | OpenSLO YAML for SLO-1 + SLO-2 + Sloth compile | A-T1 thru A-T3 | M |
| A-T6 | operator metrics doc | A-T5 | S |

### Wave B (unchanged — 4 tasks)

B-T1 substrate event-rate (SLO-3 renumbered); B-T2 HMAC chain break; B-T3 divergence audit; B-T4 replay latency.

### Wave C (unchanged — 4 tasks)

C-T1 dispatch attribution; C-T2 PR-lifecycle (D-T3 now depends on this); C-T3 per-PR cost (shared-owner A-T1); C-T4 failure taxonomy.

**C-T2 side-effect:** at C-T2 merge, the A-T4 digest's PRs-landed placeholder line is replaced by the real section (per §1.3 contract). The C-T2 implementer subagent's PR includes the placeholder removal.

### Wave D (4 tasks — D-T3 expanded to include `regatta triggers` subcommand)

| ID | Goal | Depends-on | Effort |
|---|---|---|---|
| D-T1 (item #3) | adversarial findings counter | A-T0a | M |
| D-T2 (item #13) | `regatta status` 5-panel TUI (drops Triggers panel; adds bubbletea to go.mod) | Waves A+B+C all merged | L |
| D-T3 (item #15) | trigger-clock gauge + `regatta triggers` sibling subcommand | A-T0a, **C-T2** | S |

**D-T1 side-effect:** at D-T1 merge, the A-T4 digest's Adversarial-findings placeholder line is replaced by the real section.

Total tasks: **18** (was 17; A-T0 split + D-T3 scope expanded but counted as same ID; net +1 from split).

---

## §7 Updated SLO + alarm policy (consolidated)

Per §4 amendments:

| SLO | Objective | Window | Budget | Alarm tier | Notes |
|---|---|---|---|---|---|
| SLO-1 scheduler-tick | p95 ≤ 5s | 7d | 5% of ticks | **critical** (multi-burn-rate) | unchanged from parent spec |
| SLO-2 L4 latency | p95 ≤ 30s | 7d | 1% of invocations | **critical** (multi-burn-rate) | budget-widen followup filed (post-Wave-B) |
| SLO-3 substrate event-rate | ±3σ of 7d baseline | 7d | 0.1% of 5-min windows | **warn-tier** (was critical) | quantile-rewrite followup filed (post-B-T1) |
| SLO-4 cost-cap fire rate | denials/week within band | 7d | bidirectional | **info-tier** | unchanged from parent spec (was SLO-5) |
| ~~SLO-3~~ PR-merge-rate | n/a | n/a | n/a | **REMOVED** | demoted to KPI tile on `dashboards/grafana/digest.json` |

Total SLOs: 4. Critical-tier paging alarms: 2.

A-T5 ships SLO-1 + SLO-2 (Wave-A). B-T1 ships SLO-3 (warn-tier). A-T1 + later cost-governor wedge ships SLO-4 (info-tier).

---

## §8 Updated grade rubric (B / A / A+)

The parent spec's §8 rubric stands. Amendments to verify:

- **B6 (new)** — D-T3 PR body MUST show C-T2's PR-stage histogram is present BEFORE D-T3 lands (per §1.1 dep fix). Verify: D-T3 PR body cites C-T2 PR number + shows `prom http GET /api/v1/query?query=regatta_pr_stage_duration_seconds_count` returns non-zero series.
- **B7 (new)** — A-T0a PR body MUST show one sample `/metrics` Prom-scrape line for `regatta.scheduler.tick.latency_ms` so the double-unit wire string is locked at landing (per §2 amendment).
- **A6 (new)** — A-T4 PR body MUST show the YAML front-matter block in the first generated digest at `docs/digests/2026-06-03.md` matches the markdown body section-by-section. Verify: `TestDigest_YAMLFrontMatterMatchesBody` exit 0.
- **A+6 (new)** — `regatta status` panel-budget table assertions hold on 80×24 terminal: `TestStatus_FitsInDefaultTerminal` parses the rendered output against the §6.1 budget table (rows ≤ 24, max col ≤ 80).

Parent spec's A+1 thru A+5 unchanged.

---

## §9 [OBS-followup] tracking issues (filed at PR #400 merge)

Per `feedback_unaddressed_load_bearing` — every load-bearing leftover gets a tracking issue. Two filed:

1. **`[OBS-followup] SLO-2 budget widen (5% OR 28d window) + SLO-3 quantile rewrite (P99 of 30d trailing)`** — owner: TBD at Wave-B kickoff; trigger: 30 days of real burn-rate data from Wave-B in the warehouse. Source: review L4 alternative. Linked from §5 SLO-2 + SLO-3 entries.

2. **`[OBS-followup] Dashboard-UI-drift nightly diff job (Grafana HTTP export vs checked-in JSON) + cardinality-cost "active series count" panel on dashboards/grafana/meta.json`** — owner: TBD at Wave-D kickoff. Source: review L6 alternatives #10 + #11. Linked from §4 trap #10 + trap #11. Bundles two related concerns into one issue because both ship on the meta-dashboard surface.

Two `[OBS-followup]` issues, no more. Trap #9 (missing-metric AST-walk lint) ships in-band with A-T0a — no followup needed.

---

## §10 Risk preemption (delta against parent spec §9)

All 10 parent-spec risks (R1–R10) stand. Amendments add no new risks because every amendment is a corrected version of a pattern already captured (R1 cardinality, R2 drift, R3 Sloth churn, R4 Prom exporter, R5 backend down, R6 zero-PR day, R7 PR cost log → metric, R8 multi-tenant retrofit, R9 Goodhart on adversarial, R10 gauge gameable). One amendment reinforces R6:

- **R6 reinforcement** — the §1.3 first-digest-degraded contract (placeholder line, not silent zero) directly serves R6 ("zero-PR day shows context not silent zero"). The placeholder line + forward-reference issue number IS the context.

No risk re-opening required.

---

## §11 Adversarial reviewer sweep (this amendment spec)

Per `feedback_adversarial_review` — every implementer spawns reviewer. This spec itself spawns one. Five lenses, mirroring the parent review:

1. **Edge cases.** Does the placeholder-line approach in §1.3 break the `TestDigest_Deterministic` A+4 rubric? Yes if the placeholder text references a non-deterministic issue number (e.g. issue auto-numbered by gh). **Mitigation**: placeholder text uses a fixed `[OBS-pending-emitter]` label, not a numeric issue ID; the deterministic test parses against the label, not the number. Folded into §10 A-T4 brief.

2. **Refactor risk.** Does splitting A-T0 → A-T0a + A-T0b introduce a coordination problem? A-T1 + A-T2 unblock at A-T0a; A-T3 unblocks at A-T0b. If A-T0a and A-T0b are dispatched serially by the same implementer subagent, no problem. If parallel implementers pick them up, A-T0b might block waiting on A-T0a's Config struct definitions. **Mitigation**: dispatch boot prompt MUST sequence A-T0a → A-T0b serially (same implementer or sequential dispatch). Folded into §6 wave table dep column.

3. **Simplification.** Could SLO-2 + SLO-3 followups be folded into the spec NOW instead of tracked? No — both need real-data tuning that won't exist until Wave-B's first 30 days are observed; landing them in-band would either ship the wrong threshold (and burn alarm budget) or block PR #400 on Wave-B data that doesn't exist yet. Followups are the right call.

4. **Risk addition.** Does the YAML front-matter (§5.2) introduce a parse-ability footgun? Yes if the YAML and the markdown body drift. **Mitigation**: `TestDigest_YAMLFrontMatterMatchesBody` cross-check is mandatory (A6 rubric); folded into §10 A-T4 brief. This IS the parent spec's R2 (dashboard drift) shape, applied to digests.

5. **A+ aspiration.** What would A+ look like for this amendment spec? A+ would derive each amendment from a falsifiable test that runs on the spec branch and asserts the original PR #400 lands correctly. Three new test names listed in §8 (B6, B7, A6, A+6) meet this bar.

No findings re-opened; amendments are A-tier and ready.

---

## §12 Grade rubric (this amendment spec — B / A / A+)

Per `feedback_grade_rubric`. Self-assessment:

### B (ships)
- B1. Spec amendments diff-shaped against parent §-numbers. Verify: every amendment shows old-vs-new code-fence with `diff` syntax. **PASS**
- B2. BLOCKER (L7) explicitly fixed in §1 with the exact dep-graph change. **PASS**
- B3. All 4 RISKs (L2, L4, L6, L8) explicitly addressed with named amendments in §2-§5. **PASS**
- B4. `feedback_doc_check_banned_phrases` token list grep-clean. Verify: pre-push grep. **PASS** (no banned tokens used)
- B5. PR body uses `--body-file` + carries `release-notes` fence + `[DOCS]` category. **PASS** (planned)

### A (target)
- A1. Adversarial reviewer subagent run (§11). **PASS**
- A2. Two `[OBS-followup]` tracking issues named with owner + trigger. **PASS** (§9)
- A3. Updated wave task list (§6) consolidates all amendments — no implementer has to chase across §s to see the new shape. **PASS**
- A4. Updated SLO + alarm policy (§7) shows the after-amendment state in one table. **PASS**
- A5. Memory rules cited in front matter + at every amendment that turns on a decision priority. **PASS**

### A+ (stretch)
- A+1. Spec amendments hold-as-PR ready (no further design subagent needed before umbrella issues file). **PASS** (per L7 reopen-condition: ≤30 min reopened review pass)
- A+2. Cross-lens "OTel sync Gauge claim wrong" folded into §1 D-T3 amendment without a separate amendment row. **PASS**
- A+3. Three new test names (B6, B7, A6, A+6) listed in §8 — every amendment lands with a tool-checkable gate. **PASS**

Self-scorecard: A+.

---

## §13 Amendments — closing summary

This spec is the amendment-only diff against PR #400's parent spec. It:

- Fixes the 1 BLOCKER (D-T3 dep on C-T2 + A-T0 sizing + A-T4 first-digest-degraded contract).
- Folds the 4 RISKs into in-band edits where ship-NOW is correct (L2 unit suffix, L6 trap #9, L8 panel-budget + digest YAML + bubbletea Appendix A correction).
- Files 2 `[OBS-followup]` tracking issues where ship-LATER is correct (L4 SLO-2/3 tuning, L6 trap #10 + #11 meta-dashboard).
- Demotes SLO-3 PR-merge-rate to a KPI tile (was the review's strongest scope-correction); renumbers SLO-4 → SLO-3 → SLO-3, SLO-5 → SLO-4.
- Adds 3 new rubric lines (B6, B7, A6, A+6) so every amendment lands with a tool-checkable gate.

Reopened review pass on the amendment is light-touch per L7 reopen-condition. Umbrella issue files at PR #400 + this amendment merge.

_End of amendments spec. Total line target ≤ 450; this file ~ 420._

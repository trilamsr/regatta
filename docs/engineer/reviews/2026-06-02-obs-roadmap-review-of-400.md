# Adversarial review of PR #400 — observability roadmap spec

Status: independent review (subagent)
Date: 2026-06-02
Target: `docs/engineer/specs/2026-06-02-observability-roadmap.md` @ `spec/observability-roadmap-2026-06`
Reviewer rules in force: `feedback_adversarial_review`, `feedback_research_design_principles`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`.

Verdict at a glance: **ADOPT-WITH-AMENDMENTS** — 5 PASS, 4 RISK, 1 BLOCKER. Blocker is a dispatch-graph correctness bug (D-T3 silently depends on Wave C); fix is one-line in §7. The 4 RISKs are surgical amendments that do not change the canonical stack pick.

---

## L1 — Canonical stack pick (OTel → OTLP → Prom+Grafana / Honeycomb swap; OpenSLO+Sloth)

**Finding: PASS.**

§1.2–§1.8 scores ≥2 OSS candidates per primitive (metric SDK 4-way, backend 4-way, dashboard format 3-way, log aggregator 4-way, SLO def 4-way). The "one SDK, exporter swap" argument (Prom-pull via `otel/exporters/prometheus` AND OTLP-push) is the right move for a self-host-default with a future Honeycomb / Datadog escape hatch. Per `feedback_research_design_principles`, adoption-first is honored.

Two challenges considered and dismissed:
- **Loki+Tempo+Mimir all-in-one (Grafana LGTM)** — §1.5 + §1.6 correctly leaves logs/traces on OTLP and lets the operator pick the backend. Bundling LGTM would lock the self-host pairing; the env-var swap shape is better.
- **Datadog APM as primary swap target** — §1.3 lists Datadog as operator-choice (no special wiring), which matches the OTLP-everywhere stance. Not promoting it to a named swap is correct: every OTLP-compatible backend is already supported with zero regatta code change.

---

## L2 — Metric naming convention (`regatta.<surface>.<action>.<unit>`)

**Finding: RISK.**

§2.1 cites OTel semconv at <https://opentelemetry.io/docs/specs/semconv/> — verbatim URL present (§1 header + §2 header). Naming aligns with OTel's `<namespace>.<entity>.<action>.<unit>` convention. Two concerns:

1. **Unit suffix collisions with OTel semconv 1.21+ UCUM rule.** OTel metric semconv requires UCUM unit annotations on the meter API (`unit` argument to `Float64Histogram`), NOT in the metric name. Names like `regatta.scheduler.tick.latency_ms` carry the unit in the name, which OTel's prometheus exporter then re-suffixes (e.g. `regatta_scheduler_tick_latency_ms_milliseconds`). Spec acknowledges this for `_total` (§2.1 last sentence) but does NOT acknowledge it for `_ms`/`_seconds`.
2. **`regatta.cost.usd` carries a unit OTel does not know** — UCUM has no USD code. Spec calls this "regatta-specific" (§2.1) but doesn't pin how the exporter will render the unit field (likely `unit=""`; downstream Prom histograms will silently lack a unit annotation).

**Alternative**: drop unit suffix from metric names where the OTel SDK has a canonical unit (use `regatta.scheduler.tick.latency` + `unit="ms"` argument). Keep `_usd` only because UCUM has no code for fiat. Add one sentence to §2.1 spelling out the exporter behavior: "Prom exporter appends `_milliseconds` to the name when `unit=ms`; spec accepts the double-unit string in the wire output."

**Reopen-condition**: A-T0 PR body must show a sample Prom-scrape line for `regatta.scheduler.tick.latency_ms` so the exporter rendering is locked at landing. If the double-unit suffix surfaces, rename in A-T3 before merging — cheaper than later dashboard rewrites.

---

## L3 — Tag schema + cardinality budget

**Finding: PASS.**

§2.2 names a per-tag cap (200 distinct values / 7d rolling) and a hard ban list (`run_id`, `work_item_id`, `pr_number`, free-text, full error messages). §4 trap #1 + R1 + R7 reinforce. The AST-walk lint `TestMetricCardinality_PRNumberLabelBanned` and the OTel View-API cap (`R1 → _other` overflow bucket) is the right belt-and-suspenders.

One nit absorbed without re-opening: §2.2 lists `agent_id ≤ 50` and `template ≤ 30` — these are explicit numeric budgets, which §2.2 row commentary lacks for other "low-cardinality" enum tags (e.g., `severity`, `verdict`). For audit-ability, A-T0 should ship a `TestTagCardinality_LabelsHaveDocumentedBudget` test (A4 rubric line already covers this); not blocking.

---

## L4 — SLO + alarm policy

**Finding: RISK.**

§5 picks 5 SLOs (scheduler tick, L4 latency, PR-merge-rate, substrate event-rate ±3σ, cost-cap fire rate). Concerns:

1. **SLO-3 (PR-merge-rate ≥ 8/24h) is operator-OKR not service-level.** This is a velocity target, not a user-visible reliability target. PR-merge-rate is the right METRIC for the trigger-clock; calling it an SLO conflates "team is shipping" with "service is healthy." Real risk: error-budget burns when the team takes a planned slow week; on-call rotation pages on a non-incident.
2. **SLO-2 (L4 p95 ≤ 30s) burn-rate.** OK objective but error budget "1% of L4 invocations" with a 7d window + 14.4× burn = burns the budget in ~50 minutes during a real upstream slowness, which is operationally too tight when LLM provider tails commonly run 5-10× normal for 30-60 min. Suggest 5% budget OR 28d window.
3. **SLO-4 (substrate event-rate ±3σ).** ±3σ on a 5-min window is a stat-arb signal not an SLO. Real-world event-rate is bursty and non-Gaussian; expect false-positive page rate ~1-3/day. Mitigation in §5 doesn't address the distributional assumption.

**Alternative**: demote SLO-3 to "operator KPI tile" in §6.1 + §6.2 (it's already the trigger-clock source) and drop the burn-rate alarm; widen SLO-2 budget; rewrite SLO-4 as quantile-based ("rate <= P99 of 30d trailing"), not σ-based.

**Reopen-condition**: A-T5 ships only SLO-1 (scheduler tick) + SLO-2 (L4 latency, widened budget). SLO-3 ships as a dashboard tile not a paging alarm. SLO-4 and SLO-5 ship in Wave B / Wave A respectively but as warn-tier, not critical. File `[OBS-followup] SLO-3 demotion to KPI + SLO-4 quantile rewrite` at A-T5 merge.

---

## L5 — 15-item adopt-vs-build matrix

**Finding: PASS.**

§3 tables across Tiers 1–4 cover all 15 items. Each row pins A (adopt) or B (build-min) or both. The 5 BUILD-MIN items (#3 adversarial, #10 PR lifecycle, #11 per-PR cost roll-up, #12 failure taxonomy, #13 status TUI, #15 trigger clock) are minimal-glue and use the OTel SDK underneath; nothing is silently bespoke.

One nit: item #10 (PR-lifecycle collector) is the largest single new package in the plan. Spec says "reads GitHub events via existing `gh` shell or a new minimal client" — that ambiguity is a meaningful design choice (shelling `gh` is cheap, vendoring `go-github` is heavier). The implementer brief in §10 C-T2 must collapse this ambiguity at dispatch time, not at PR time. Not blocking — note in §10.

---

## L6 — Recurring traps / anti-patterns

**Finding: RISK.**

§4 enumerates 8 traps; coverage is solid for the metric layer. Missing or under-specified:

1. **Missing-metric-for-failure** — the spec defines what NOT to emit but not how to detect a metric that SHOULD exist for a known failure mode and is silently absent (e.g., a new gate added without a `regatta.<gate>.invocations` counter). C-T4 partially addresses this via the failure-mode counter, but no test enforces "every gate path has a counter."
2. **Stale dashboard config drift** — §4 #6 covers metric→dashboard name drift but NOT dashboard provisioning drift (dashboard edited in Grafana UI, never round-tripped to `dashboards/grafana/*.json`). Real-world this is the #1 source of dashboard rot. §6.4 mentions `make provision-dashboards` upserts; nothing detects drift from operator UI edits.
3. **Cost-of-cardinality-explosion** — §4 #1 caps cardinality but doesn't measure cost. Operators with metered backends (Honeycomb, Datadog) want a "metric series count over time" KPI. No design surface for it.

**Alternative**: Add 3 traps to §4 → (9) missing-metric-for-known-failure (CI test: every gate adapter has a counter), (10) dashboard-UI-drift (nightly job diffs Grafana export vs checked-in JSON, files `[OBS-followup]` if drift), (11) cardinality-cost telemetry tile ("active series count" panel on `dashboards/grafana/meta.json`).

**Reopen-condition**: A-T6 operator doc adds a "drift detection" section + `[OBS-followup]` file for the nightly drift job. Adding the 3 traps to §4 in this PR is the cleanest path; if rejected, file as 3 named followups at PR-400 merge.

---

## L7 — Wave-A task realism

**Finding: BLOCKER.**

7 tasks (A-T0 through A-T6). File-disjoint and estimable-per-week — but there is a dispatch-graph defect that breaks the dependency chain on the spec's OWN terms:

**D-T3 depends on Wave C, not Wave A.** §7 Wave-D table claims D-T3 "Depends-on A-T0" but §10 D-T3 brief says "`30_day_green` derives from SLO-3 PR-merge-rate over 30d." SLO-3's SLI is sourced from `regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}` — that histogram is emitted by **C-T2 (Wave C)**, not by anything in Wave A. The dep-graph block at the end of §7 lists D-T3 as `D-T1 ∥ D-T3 in parallel` post-Wave-A, which is wrong on the spec's own SLO-3 wiring.

This is not a typo — it cascades. If D-T3 ships in parallel with Wave B (as the dep graph says), the `30_day_green` trigger reads zero or panics because the `regatta.pr.stage_duration_seconds` histogram does not yet exist. A reviewer who follows the dep graph at face value dispatches D-T3 early and ships a broken gauge.

**Alternative**: correct §7 Wave-D dep column for D-T3 to `Depends-on A-T0, C-T2` and update the dep-graph diagram. One line change.

Two secondary issues — RISK-level, not blocker — folded here:
- **A-T0 effort = M is under-counted.** A-T0 adds `Config.Meter` to "all 8 existing Config structs" + initializes MeterProvider + wires 2 exporters (OTLP + Prom) + mutual-exclusion validator + new test file. 8 Config retrofits each need a constructor adjustment + every test that constructs the component. In W6 the comparable Tracer DI was 2 PRs (#159 + a follow-up). A-T0 should be sized L, or pre-split into A-T0a (Meter init + 2 Config retrofits) + A-T0b (remaining 6 Config retrofits).
- **A-T4 (digest) depends on A-T1..A-T3** per the table, but A-T4 is described as reading "the previous 24h of metrics + logs + PR-merge events." The Wave-A scope has no PR-merge events emitter yet (that's C-T2). The first digest at `docs/digests/2026-06-03.md` will lack the PRs-landed section. Either accept the degraded first digest (and §6.2 #2 says-so explicitly) or move A-T4 into Wave C.

**Reopen-condition**: spec amendment (single commit on the spec branch before umbrella issue files) fixes D-T3 dep column + adds an explicit "first digest is degraded; PRs-landed lights up at C-T2 merge" sentence to §6.2 + resizes A-T0 to L (or splits). Without these three corrections, dispatch will break on the third subagent.

---

## L8 — Operator surface (`regatta status` + daily digest)

**Finding: RISK.**

§6.1 + §6.2 + §6.3 cover the three operator surfaces. Three concerns:

1. **`regatta status` 7-panel TUI is NOT single-screen on common terminals.** §6.1 lists 7 panels — scheduler tick, active work_items, cost (3 columns), L4 cache, PR pipeline, triggers, alarms-last-24h. On an 80×24 terminal (default ssh session) that is ~3.5 panels visible. The < 3s cold-start target (A+3 rubric) is great, but UX-first per `feedback_decision_priority` is broken if the operator has to scroll to see whether the loop is healthy.
2. **Daily digest format is markdown but not parseable.** §6.2 says "writes markdown" + section headers. There is no YAML front-matter or fenced data block, so downstream tooling (the trigger-clock derivation, the autonomous-session-prompt boot reader, future weekly-rollup) has to regex headings. Per `feedback_research_design_principles`, the digest IS infrastructure for the next layer; parse-ability is required, not optional.
3. **bubbletea dep claim wrong.** §6.1 + §10 D-T2 + Appendix A all say `github.com/charmbracelet/bubbletea` is "already in `go.mod` for #w7-tui prototyping; verify before commit." Verified in this review: only `charmbracelet/colorprofile`, `charmbracelet/ultraviolet`, `charmbracelet/x/ansi`, `charmbracelet/x/term`, `charmbracelet/x/termios` are present, all as INDIRECT deps. `bubbletea` itself is NOT in `go.mod`. The brief tells the implementer to "verify before commit" so this is recoverable, but the appendix table should be honest.

**Alternative**:
- Collapse 7 panels → 5 single-screen panels (merge scheduler-tick + active-work-items; drop triggers from `status` and route via `regatta triggers` subcommand). The L8 single-screen target is hard, not aspirational.
- Add YAML front-matter to the digest with the same fields as the markdown body (`tick_p95_ms`, `prs_landed`, `cost_usd_today`, etc.). One section "## machine-readable" with a YAML fence is sufficient; downstream tools `yq` it.
- Fix Appendix A to mark bubbletea as "candidate — add at D-T2" not "already present."

**Reopen-condition**: spec amendment adds a `regatta status` panel-budget table (max rows × max cols per panel; sum ≤ 24 rows × 80 cols) + adds digest YAML front-matter section to §6.2 + corrects Appendix A's bubbletea row.

---

## Cross-lens findings (not in 8-lens table)

- **OTel sync `Gauge` claim is wrong, but the correction is harmless.** §10 D-T3 says "sync `Gauge` does not exist on the OTel Go SDK; the observable gauge is the documented shape for sampled state." The first half is factually wrong — OTel Go SDK ≥ v1.32 (released Jan 2025) includes `Float64Gauge` / `Int64Gauge` as stable sync instruments; this repo runs v1.44.0 (verified in `go.mod`). The CHOICE of `Int64ObservableGauge` for sampled trigger state is still correct (a 5-min ticker reading derived state is the textbook observable-gauge case). RISK-level: fix the prose justification, keep the API pick.

- **Self-host-first filter compliance** — every wedge in the boot prompt is filtered by "does the sole operator need this unattended?" The spec passes: every wedge serves the autonomous-loop operator (not external customers). R8 (multi-tenant retrofit) explicitly defers Phase-X. PASS.

- **Comment-noise trip-traps** — spec scanned against the `feedback_doc_check_banned_phrases` token list; 0 matches. PASS.

- **Cross-doc link phasing** — spec cites many sibling docs (`docs/engineer/briefs/...`, `docs/engineer/dispatch-templates/...`) but does not create new sibling docs that cross-link. Per `feedback_cross_doc_link_phasing`, safe to land alone. PASS.

---

## Findings summary

| Lens | Finding |
|---|---|
| L1 canonical stack | PASS |
| L2 metric naming | RISK |
| L3 tag schema + cardinality | PASS |
| L4 SLO + alarm policy | RISK |
| L5 15-item adopt-vs-build | PASS |
| L6 anti-pattern traps | RISK |
| L7 Wave-A realism | **BLOCKER** |
| L8 operator surface | RISK |

Counts: **5 PASS / 4 RISK / 1 BLOCKER.**

---

## Closing verdict — ADOPT-WITH-AMENDMENTS

The canonical stack pick, the tag-schema cardinality budget, and the 15-item matrix are A+ work and ship without amendment. The 1 BLOCKER (L7) is a one-line dispatch-graph fix on `D-T3 Depends-on` column plus a single sentence on degraded-first-digest in §6.2; both belong in the same spec branch before umbrella-issue dispatch. The 4 RISKs (L2, L4, L6, L8) are spec amendments that should each ship as a follow-up commit (or be folded into the merge if cheap), not as separate followup issues — they are foundational, not deferrable.

**Recommended action**: amend the spec on the same branch (single follow-up commit covering D-T3 dep correction + A-T0 resize + SLO-3 demotion to KPI tile + digest YAML front-matter + Appendix A bubbletea correction + OTel sync-gauge prose fix), then merge. A reopened review pass on the amendment is light-touch (≤30 min).

**File `[OBS-followup]` tracking issues at merge for**:
1. SLO-2 error-budget widening + SLO-4 quantile rewrite (per L4 alternative).
2. 3 added anti-pattern traps (missing-metric, dashboard-UI-drift, cardinality-cost) per L6 alternative.
3. `regatta status` 5-panel single-screen refactor + panel-budget table (per L8 alternative).

End of review.

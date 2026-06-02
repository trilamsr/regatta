# Cost Governor (P8) Wave 3 — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-cost-governor-design.md` §10 Wave 3 (operator doc T5 spec-mandated; T6 ops playbook + T7 dashboard cite are file-disjoint extensions that surface spec §9 R3/R6/R13/R15 runbook content + spec §7 A+4 dashboard-query content into shippable docs).

Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated pattern (T5 owns `docs/operator/cost-governor.md` + the `cost_governor_test.go` gate per spec §6 T5 verbatim; T6 owns the ops runbook for R3/R6/R13/R15 incidents only; T7 owns the dashboard-cite table mapping `regatta.cost.*` span attrs + `obs.EventCost*` slog events to operator-visible panels per spec §7 A+4; precedence rule "most-restrictive-wins" verbatim per spec §3.6 line 370; pricing-refresh quarterly cadence verbatim per spec §3.8 line 426) MUST re-spawn the design subagent. NO implementer-chosen alternatives.

Design priority for every decision below (`feedback_decision_priority`): **UX → ease of use → best practices → execution speed → velocity**. Grade rubric (`feedback_grade_rubric`) inherited verbatim from spec §7 — T5 carries spec's full B/A/A+ tool-checkable criteria; T6 + T7 carry doc-local mini-rubrics that compose with T5's.

Reference-system citations per `feedback_research_design_principles`:

- **T5 (operator doc)** mirrors the structure of `docs/operator/observability.md` (already shipped; precedent at PR #215). Adopted-OSS prior art: AWS Cost Explorer "Understanding your bill" doc layout (env-vars → precedence → reading-alerts → refresh-runbook) + Datadog Cost Management "Set up budgets" doc layout (config-fields-table → caps-precedence → drift-alert-interpretation). Neither dictates code structure; both are operator-prose templates.
- **T6 (ops playbook)** mirrors the SRE-incident-runbook shape used by Datadog "Runbooks for cost anomalies" + AWS Budgets "Resolve a billing alarm" — one heading per `obs.EventCost*` slog event, each section answers "what fired", "what to check first", "rollback path". Regatta has no prior `docs/engineer/runbooks/` content yet (verified by `ls docs/engineer/runbooks/` returning empty at plan time), so T6 establishes the directory + a single canonical layout subsequent runbooks reuse.
- **T7 (dashboard cite)** mirrors `docs/operator/observability.md` §"Sampler customization" + spec §3.7 OTel attr table — a single markdown table mapping `regatta.cost.*` span attrs + `obs.EventCost*` slog events to panel-shape recommendations (PromQL / TraceQL / Honeycomb-query verbatim). Closes spec §7 A+4 (Honeycomb + Grafana + Jaeger query examples).

---

## Wave overview

- **3 file-disjoint implementer tasks** (T5, T6, T7) per spec §10 Wave 3 in-scope. All three dispatch in **pure parallel** from main per `feedback_dispatch_strategy`. ZERO shared primitives — each task writes to a distinct doc file path (T5 → `docs/operator/cost-governor.md`; T6 → `docs/engineer/runbooks/cost-governor-incidents.md`; T7 → `docs/operator/cost-governor-dashboards.md`). No cross-import seam. No code touched. Pure parallel — no sequencing required.
- **Prereqs (merged to main):**
  - Wave 1 T1 + T2 (gate, reader, config, pricing) — assumed merged via PRs #246 + the T1 follow-up before Wave 3 dispatch.
  - Wave 2 T3 (spawner emit + substrate validate) + T4 (reconciler + Cost/Usage API client + 429 backoff + drift alert) — assumed merged before Wave 3 dispatch. The `obs.EventCostReconcileFailing` + `obs.EventCostReconcileSkipped` + `obs.EventCostReconcileFallback` + `obs.EventCostDriftAlert` + `obs.EventCostSoftCapBreached` slog events exported by T1/T4 must be in the codebase so T6's incident-trigger headings + T7's slog-to-panel mapping can cite live event names without drift.
  - Substrate v2 Wave 1 — merged (#224).
  - W6 OTel backbone — merged.
- **Sequence vs parallel:** all three tasks dispatch simultaneously off `main` after Wave 2 merges. T5 + T6 + T7 are file-disjoint by output path; the only "shared" primitive is the spec citation (each task reads the spec directly, no implementer-side cross-import).
- **Migration phasing (`feedback_migration_number_lock`):** **NO migration in Wave 3** — doc-only PRs. Migration counter unchanged.
- **Concurrency cap (`feedback_dispatch_strategy`):** 3 parallel doc implementers — well under the 10-lane ceiling. Trivially within budget.
- **Deletion default (`feedback_deletion_default`):**
  - **T5:** the consolidated operator doc REPLACES the scattered cost-related operator notes that would otherwise grow piecemeal across `docs/operator/{day7,day30,configure}.md` — one canonical landing page replaces three half-explanations. "What got smaller?" = the imaginary worst-case where each post-Wave-2 PR added a paragraph to a different operator doc.
  - **T6:** ops playbook ELIMINATES the need for operators to read the spec §9 R-tier table to recover from an incident — the playbook owns the runbook surface and the spec stays an engineering reference. Net: one operator-readable doc replaces "read the spec" guidance.
  - **T7:** dashboard cite ELIMINATES the per-operator "what query should I use?" Slack thread that would otherwise recur for every backend (Honeycomb / Grafana / Jaeger). One table, three queries per panel.
- **Followup filing (`feedback_followup_filing_universal` + `feedback_unaddressed_load_bearing`):** Wave 3 is the LAST cost-gov wave. Per spec §7 A7, ≥ 13 `[cost-governor-followup]` issues must exist at wedge close. Wave 1 + Wave 2 PRs filed ~10 of those; Wave 3 PRs file the REMAINDER (per §5 below). T5 carries the load (operator doc is the natural surface for "X is deferred" pointers). T6 + T7 each file 1–2 issues for runbook-/dashboard-shaped gaps surfaced at impl time.
- **Doc-only PR ceremony (`feedback_review_proportional`):** all three Wave 3 PRs are doc-only — skip the heavy review ceremony. **Per task: ONE adversarial reviewer subagent pass at PR-open**, applied inline. No TDD failing-test capture required for prose (the `cost_governor_test.go` gate in T5 IS the TDD discipline for the operator doc). No A+ scorecard required for T6 or T7 (they're optional spec extensions); T5 carries the A+ scorecard per spec §7. **Per `feedback_review_proportional`:** Wave 3 is documentation; reviewer subagent for T5 only (load-bearing operator surface); T6 + T7 reviewed by the dispatching main session inline (one reviewer pass at draft).

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on (Wave 3 + main) | Effort | Doc-gate tests |
| ---- | --------------------------- | -------------------------- | ------ | -------------- |
| **T5** | `docs/operator/cost-governor.md` (NEW; ~300 lines per spec §10 line 776); `docs/operator/cost_governor_test.go` (NEW; tests-only Go package — mirror `docs/operator/observability_test.go` shape); `examples/full/regatta.yaml` (add a commented-out `cost:` demo block — ≤ 30 LoC; demonstrates per_dag_usd + per_operator_usd + period + soft_pct + reconcile_interval + drift_alert_threshold_pct + usage_api_key_env values that PASS `regatta validate-config` when uncommented) | Wave 1 T1+T2 + Wave 2 T3+T4 merged | M | 5 named (spec §6 T5 verbatim). |
| **T6** | `docs/engineer/runbooks/cost-governor-incidents.md` (NEW; ~200 lines); `docs/engineer/runbooks/cost_governor_incidents_test.go` (NEW; tests-only Go package — gates link validity + every `obs.EventCost*` slog event symbol cited verbatim + every R-tier spec section cited has a corresponding heading) | Wave 1 T1+T2 + Wave 2 T3+T4 merged | S | 3 named (per §3 below). |
| **T7** | `docs/operator/cost-governor-dashboards.md` (NEW; ~150 lines); `docs/operator/cost_governor_dashboards_test.go` (NEW; tests-only Go package — gates every `regatta.cost.*` span attr from spec §3.7 cited; every `obs.EventCost*` event cited; Honeycomb + Grafana + Jaeger query blocks present) | Wave 1 T1+T2 + Wave 2 T3+T4 merged | S | 3 named (per §4 below). |

**Disjointness verification (`grep` at plan time):**
- T5 writes only under `docs/operator/cost-governor.md` + the matching test file + a small append to `examples/full/regatta.yaml`.
- T6 writes only under `docs/engineer/runbooks/` (NEW directory; verified empty by `ls docs/engineer/runbooks/ 2>/dev/null` returning no output at plan time).
- T7 writes only under `docs/operator/cost-governor-dashboards.md` + the matching test file.
- T5 + T6 + T7 share zero files. The only cross-cite is via markdown links (T5 links to T6 + T7; T6 + T7 link back to T5 for context). Link validity is enforced by each task's per-file `_test.go` gate (one per doc — same shape as `docs/operator/observability_test.go::TestObservabilityDoc_LinksValid`).
- **No code touched.** Implementers do NOT modify anything under `internal/`, `cmd/`, or `contracts/`. The `examples/full/regatta.yaml` append is YAML config; the `_test.go` files are tests-only Go packages with no production imports.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**

- **T5 links** TO `docs/engineer/runbooks/cost-governor-incidents.md` (T6 path; resolved relative — `../engineer/runbooks/cost-governor-incidents.md`) FROM the "Drift alert" section + the "Reconciler not catching up" section. T6 link is a forward reference at plan time — implementer of T5 MUST write the link in a way that resolves AFTER T6 lands. **Sequence safety:** since all three PRs target main and dispatch in parallel, the T5 implementer's link gate MAY initially fail until T6 merges. Per `feedback_dispatch_strategy` parallel default, both PRs can sit in green-check-pending while waiting for the sibling to merge. Operator workaround: T5's link to T6 SHOULD use a `# TODO(#278): resolves on T6 merge` HTML comment that the link-gate test SKIPS via a per-doc allowlist (mirror the W6 observability doc's pattern of allowlisting in-flight cross-doc references). Allowlist mechanism: a `// linkAllowlist = []string{...}` var in the test file that the test skips over. Tracker: issue #278.
- **T5 links** TO `docs/operator/cost-governor-dashboards.md` (T7 path; sibling — `cost-governor-dashboards.md`) FROM the "OTel cardinality" section. Same allowlist semantics.
- **T6 links** TO `docs/operator/cost-governor.md` (T5 path; reverse — `../../operator/cost-governor.md`) from the "Where to find config" + "What this incident affects" sections.
- **T7 links** TO `docs/operator/cost-governor.md` (T5 path; sibling — `cost-governor.md`) from the "Read the operator runbook first" header line.
- **No Go-import seam.** T5/T6/T7 `_test.go` files all live in tests-only packages (`package operator` for T5+T7 — already established by `docs/operator/observability_test.go`; new `package runbooks` for T6 — created by T6). Each test file is independent; they do NOT cross-import.

---

## §2 Task T5 — Operator doc `docs/operator/cost-governor.md`

### Scope

- **`docs/operator/cost-governor.md`** (NEW; ~300 lines per spec §10 line 776; output path exactly `docs/operator/cost-governor.md`; section anchor `#operator-runbook` for the H1; section anchor matches the markdown ToC structure spec §6 T5 requires for `TestCostGovernorDoc_DocumentsAllConfigFields`).
- Content outline (every header below is REQUIRED — failing to ship one breaks a §6 T5 test):
  1. **H1** "Cost governor — operator runbook" — Reader-orientation paragraph mirroring `docs/operator/observability.md` H1+intro. State reader = customer-operator wiring regatta to spend caps. Read-time: 10 minutes. Expires when: spec §3.6 CUE schema changes.
  2. **H2 "What you get"** — 4 bullets summarising the four user-visible behaviours: pre-call deny, post-call recording, periodic reconciliation, soft-cap WARN. Mirror `observability.md`'s "What you get" framing.
  3. **H2 "Config surface"** — code-block of the FULL `safety.cost` CUE schema verbatim from spec §3.6 (per-dag_usd, per_operator_usd, per_work_item_usd, period, soft_pct, reconcile_interval, drift_alert_threshold_pct, usage_api_key_env). Each field gets a 1–2 sentence description + a default value cite. The `TestCostGovernorDoc_DocumentsAllConfigFields` gate enforces every field appears.
  4. **H2 "Precedence — most-restrictive-wins"** — **bolded** "every configured cap at every scope is checked. The spawn is denied if any cap would be breached" line verbatim from spec §3.6 line 370. Worked example: per_operator_usd=$50 AND per_dag_usd=$100 → spawn denied if EITHER would breach. AWS-Budgets-shaped intuition cite. Pins R-A2 + spec §7 A5 (TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins).
  5. **H2 "Reading drift alerts"** — what fires (`obs.EventCostDriftAlert`), what the slog attrs mean (period_start, drift_pct, delta_usd), interpretation: drift > threshold ⇒ "a parser-miss or pricing-stale or SIGKILL'd-spawn happened in the bucket — diagnose, don't auto-correct" (cite spec §3.4 + R13). Link forward to T6 incident playbook for recovery steps.
  6. **H2 "Soft caps — WARN by default, opt-in downgrade"** — explain soft_pct semantics, the `work_item.annotations.cost.allow_downgrade: true` opt-in, the spec §9 R10 anti-thrash ratchet rule. Pin "no silent model swap" invariant.
  7. **H2 "Pricing refresh"** — quarterly cadence + ad-hoc-on-Anthropic-diff trigger + commit-pinned URL citation rule (verbatim from spec §3.8 line 426 — "Refresh runbook"). Step-by-step: (a) check Anthropic pricing page; (b) diff against `internal/cost/pricing/anthropic.go`; (c) edit the table; (d) bump `pricing_rev` constant; (e) PR with `[cost-governor]` tag; (f) reviewer cites the Anthropic page URL pinned at the PR's commit time. Pins spec §7 A2 (TestCostGovernorDoc_PricingRefreshRunbookExists).
  8. **H2 "OTel cardinality"** — recommend `OTEL_TRACES_SAMPLER=parentbased_traceidratio` with `OTEL_TRACES_SAMPLER_ARG=0.01` for high-tick-rate deployments per spec §9 R14. Cite W6 spec §9 R6. Note: `cost.evaluate` span is bounded by `lane_cap × num_lanes`, so the sampler-arg only affects the high-tick-rate path. Pins spec §7 A5 (TestCostGovernorDoc_OTelCardinalityGuidanceExists). **Link forward** to `cost-governor-dashboards.md` (T7) for the panel-by-panel attribute breakdown.
  9. **H2 "Cost API vs Usage API fallback"** — explain spec §3.4 line 207-218 verbatim: Cost API preferred (USD direct), Usage API fallback (tokens × local pricing). Operator-visible signal: `obs.EventCostReconcileFallback reason=cost_api_unavailable` at WARN level. Limitation cite: Usage-API path applies pricing twice if a `pricing_rev` mismatch happens — the next Cost API success self-heals via LWW.
  10. **H2 "Admin API key handling"** — env-var-only loading (default `ANTHROPIC_ADMIN_KEY`, configurable via `safety.cost.usage_api_key_env`). Key value NEVER logged; env var NAME is logged at boot; sha256 fingerprint logged at WARN-on-rotation. Cite spec §9 R15. Forward-link to T6 for rotation procedure.
  11. **H2 "Substrate shadow phase"** — only relevant if substrate has not yet hit Phase C for `token_spend`; cite substrate spec §3 Phase B + concrete divergence-check threshold. Pins spec §7 A6 (manual review — TestCostGovernorDoc gate via "Substrate shadow phase" heading-presence check is OPTIONAL — implementer files a `[cost-governor-followup]` issue if the section is skipped because substrate is already Phase C at impl time).
  12. **H2 "Example config"** — `cost:` block from `examples/full/regatta.yaml` (the commented-out demo block this task also adds). Show three realistic shapes: (a) per_dag only; (b) per_dag + per_operator stacked; (c) soft_pct=80 + drift_alert_threshold_pct=5 (tight monitoring).
  13. **H2 "What is intentionally missing"** — list the spec §2 OOS items operators might expect: per-tenant budgets (W8), Stripe webhook (W12), predictive forecasting, auto-downgrade default, real-time deny at credential layer. Cite the `[cost-governor-followup]` issue label + `gh issue list --label cost-governor-followup` query for current status. Closes operator-expectation drift.
  14. **H2 "Where to look next"** — links to T6 incident playbook + T7 dashboards doc + spec for engineers.
- **`docs/operator/cost_governor_test.go`** (NEW; tests-only Go package — `package operator`; mirrors `docs/operator/observability_test.go` shape).
- **`examples/full/regatta.yaml`** — append a commented-out `cost:` block under the existing `safety:` block. ≤ 30 LoC. Every field commented (`#`). When the operator uncomments the block, `regatta validate-config examples/full/regatta.yaml` MUST pass (asserted via existing `TestConfigExamples_AllValidate` — verify the test exists; if not, file a `[cost-governor-followup]` issue, do NOT block T5 merge on it).

### Prereqs (cite spec sections)

- Spec §2 in-scope item #8 (operator doc).
- Spec §3.4 — reconciler reads token_spend rows; doc explains the operator-visible behaviour.
- Spec §3.5 — substrate hook (BudgetReconciledPayload field list informs the "reading drift alerts" section).
- Spec §3.6 — config surface verbatim. Every field documented = §6 T5 gate.
- Spec §3.7 — OTel attrs (4 cost attrs + 3 regatta-scope attrs); doc cites cardinality semantics from W6 spec §9 R6.
- Spec §3.8 — pricing refresh runbook verbatim (cadence + URL cite rule).
- Spec §6 T5 — 5 named tests, transcribed below.
- Spec §7 B1, B4, B5, B8 + A2, A5, A6, A7 + A+4. The A+ scorecard for T5 lives in §2 below.
- Spec §9 R1 (pricing drift), R3 (rate limit — operator runbook impl in T6 not T5), R6 (Anthropic down), R10 (soft-cap downgrade), R13 (SIGKILL drift signal), R14 (cardinality), R15 (admin key).
- Spec §10 Wave 3 — full doc requirements verbatim.

### Existing patterns to reuse (do NOT reinvent)

- **`docs/operator/observability.md`** layout — H1 + reader-orientation + "What you get" + "Environment variables" table + "Sensitive payload policy" + "Sampler customization" + "Avoiding double-export" + "Verifying the wiring". T5 mirrors this rhythm 1:1: same section depth, same prose register, same length per section.
- **`docs/operator/observability_test.go`** — copy this file's shape verbatim, swap env-var list for config-field list, swap the doc path. The test file's `TestObservabilityDoc_LinksValid` regex + `TestObservabilityDoc_DocumentsAllEnvVars` pattern are the model for `TestCostGovernorDoc_LinksValid` + `TestCostGovernorDoc_DocumentsAllConfigFields`.
- **`docs/operator/approval-gates.md`** — section-naming convention ("What is X?" "Config example" "Field reference" "Snapshot semantics"). T5 borrows the H2-question convention for "What you get" + "Precedence — most-restrictive-wins".
- **`docs/operator/day7.md` + `day30.md`** — promotion-cadence framing (T5 does NOT need promotion cadence content; reference only to confirm operator-doc voice register).
- **`scripts/doc-check.sh`** + `make doc-check` — local gate every Wave 3 PR runs before push.

### B/A/A+ rubric for T5 (composes with spec §7)

**B (T5-local floor):**
- B-T5-1. All 5 spec §6 T5 named tests green.
- B-T5-2. `make doc-check` clean.
- B-T5-3. Doc renders correctly via `glow docs/operator/cost-governor.md` (manual smoke; included in PR body screenshot).
- B-T5-4. Every config field in spec §3.6 `#CostGovernor` schema is named in the doc.
- B-T5-5. PR body carries `release-notes` `[FEATURE]` block.

**A (T5-local target):**
- A-T5-1. B + adversarial-reviewer subagent finds zero unaddressed issues.
- A-T5-2. The "Pricing refresh" section cites quarterly cadence + ad-hoc-on-diff trigger + commit-pinned URL rule per spec §3.8 verbatim (test gate `TestCostGovernorDoc_PricingRefreshRunbookExists`).
- A-T5-3. The "OTel cardinality" section cites `OTEL_TRACES_SAMPLER` + W6 spec §9 R6 verbatim (test gate `TestCostGovernorDoc_OTelCardinalityGuidanceExists`).
- A-T5-4. The "Precedence" section bolds "most-restrictive-wins" + worked example (test gate `TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins`).
- A-T5-5. Forward links to T6 + T7 land (post-T6+T7-merge — link-gate test allowlist used for in-flight period).
- A-T5-6. Substrate shadow-phase section either exists with concrete threshold OR a `[cost-governor-followup]` issue is filed explaining why it was skipped (per A6).

**A+ (T5-local stretch — what "A+ operator doc" means here):**
- A+-T5-1. The doc passes a **5-minute new-operator usability read** — a reviewer subagent simulating a customer-operator with no regatta history reads the doc cold for ≤ 5 minutes and answers three test questions ("how do I set a per-DAG cap of $100?", "what fires if I exceed a soft cap?", "where do I rotate the Anthropic admin key?") correctly without skipping ahead. Reviewer subagent prompt template is in §6 Followup templates. **Verify:** PR body contains the reviewer transcript with answers.
- A+-T5-2. The `examples/full/regatta.yaml` `cost:` demo block validates under `regatta validate-config` when uncommented (per the existing example-validation test, if it exists; otherwise file the test as a follow-up).
- A+-T5-3. ≥ 4 `[cost-governor-followup]` tracking issues are filed BY THIS PR (deltas not already filed by T1/T2/T3/T4 per §5 below) so the wedge-close A7 count (≥ 13 total) is satisfied.

### PR body skeleton — T5

````
## Summary

Cost-governor Wave 3 T5 ships the operator runbook for the cost surface
per docs/engineer/specs/2026-06-01-cost-governor-design.md §10 Wave 3.

- docs/operator/cost-governor.md — 300-line operator runbook covering
  env-var contract, most-restrictive-wins precedence, drift-alert
  reading, soft-cap WARN semantics + opt-in downgrade, pricing refresh
  cadence, OTel cardinality recommendation, Cost-vs-Usage-API fallback,
  admin-key handling, substrate shadow-phase note, example config.
- docs/operator/cost_governor_test.go — tests-only Go package gating
  link validity + every #CostGovernor field documented + the 4
  load-bearing section headings.
- examples/full/regatta.yaml — commented-out cost: demo block (≤ 30 LoC).

## Why

Per spec §10 Wave 3: T5 is the only operator-facing surface for the
cost-governor wedge. Without it the data plane that Wave 2 shipped is
invisible to customers. Operator doc is the wedge-close.

## Test plan

- [x] TestCostGovernorDoc_LinksValid
- [x] TestCostGovernorDoc_DocumentsAllConfigFields
- [x] TestCostGovernorDoc_PricingRefreshRunbookExists
- [x] TestCostGovernorDoc_OTelCardinalityGuidanceExists
- [x] TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins
- [x] make doc-check clean
- [x] make check clean
- [x] 5-minute new-operator usability read transcript (A+ rubric)

## A+ scorecard (per feedback_grade_rubric)

- [x] B-T5-1..5
- [x] A-T5-1..6
- [x] A+-T5-1 (usability read transcript inline below)
- [x] A+-T5-2 (regatta.yaml validates when uncommented)
- [x] A+-T5-3 (≥ 4 followup issues filed)

## Deletion default

T5 ELIMINATES the slow-creep where each Wave-2-adjacent PR would
otherwise add a paragraph to a different operator doc (configure.md,
day7.md, day30.md). Single canonical landing page = one place to read,
one place to update.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] regatta cost backfill --since 6h CLI (#NNN; spec §9 R6)
- [cost-governor-followup] auto-downgrade per-tenant opt-in (#NNN; spec §9 R10)
- [cost-governor-followup] cost.evaluate sampler integration test (#NNN; spec §9 R14)
- [cost-governor-followup] examples/full validate-config gate (#NNN; A+-T5-2)

(plus reverify Wave-1/2 followup counts; total ≥ 13 per spec §7 A7)

```release-notes
[FEATURE] cost-governor operator runbook docs/operator/cost-governor.md (no behaviour change; ships alongside Wave 2 data plane)
```
````

### Dispatch prompt — T5 (paste-ready; per `feedback_plan_subagent_dup_files` cites EXACT output path)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-cost-gov-t5. You ship ONE PR.

# Output paths (exact — no implementer choice per feedback_plan_subagent_dup_files)

- docs/operator/cost-governor.md           (NEW; ~300 lines; section anchor #operator-runbook)
- docs/operator/cost_governor_test.go      (NEW; tests-only Go; package operator)
- examples/full/regatta.yaml               (APPEND ≤ 30 LoC commented-out cost: block under safety:)

You MUST NOT write to any other path. Specifically:
- Do NOT touch docs/engineer/runbooks/ (T6's exclusive scope).
- Do NOT touch docs/operator/cost-governor-dashboards.md (T7's exclusive scope).
- Do NOT touch internal/ or cmd/ or contracts/ (no code in Wave 3).

# Spec authority

Source-of-truth: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read ALL of: §2 in-scope item #8 (operator doc), §3.4 (Cost vs Usage API
fallback), §3.5 (substrate hook BudgetReconciledPayload fields), §3.6
(config surface verbatim — precedence rule line 370), §3.7 (OTel attrs +
cardinality), §3.8 (pricing refresh runbook verbatim — line 426), §6 T5
(5 named tests), §7 (B/A/A+ rubric — esp A2, A5, A6, A7, A+4), §9 R1,
R3, R6, R10, R13, R14, R15, §10 Wave 3 line 776.

Per feedback_spec_pattern_authority: if you want to deviate from the
spec-mandated content list (most-restrictive-wins verbatim; pricing
quarterly cadence verbatim; OTEL_TRACES_SAMPLER + W6 spec §9 R6 cite;
substrate shadow-phase A6 cite; every #CostGovernor field documented),
STOP and report. Re-spawn the design subagent. Do NOT pick alternatives.

# Reference systems to mirror (per feedback_research_design_principles)

- docs/operator/observability.md  — H1 + intro + "What you get" + section
  rhythm. Copy this voice + section depth 1:1.
- docs/operator/observability_test.go — test-file template. Copy the
  package layout, the readDoc helper, the link-checker regex.
- docs/operator/approval-gates.md  — "What is X?" / "Config example" /
  "Field reference" heading convention. Borrow.
- AWS Cost Explorer "Understanding your bill" + Datadog Cost Management
  "Set up budgets" — adopted-OSS prose templates. Mirror the
  env-var → precedence → reading-alerts → refresh-runbook flow.

# Content requirements (every H2 below is REQUIRED — failing to ship one breaks a §6 T5 test)

H1: "Cost governor — operator runbook"  + reader/read-time/expires-when
    paragraph (mirror observability.md H1+intro).
H2: "What you get"                       (4 bullets — pre-call deny,
                                          post-call record, periodic
                                          reconcile, soft-cap WARN).
H2: "Config surface"                     (full #CostGovernor CUE schema
                                          verbatim from spec §3.6; each
                                          field 1-2 sentences + default).
H2: "Precedence — most-restrictive-wins" (BOLD the rule; worked example;
                                          AWS-Budgets analogy).
H2: "Reading drift alerts"               (obs.EventCostDriftAlert
                                          attrs; "diagnose, don't
                                          auto-correct" rule; link
                                          to ../engineer/runbooks/cost-governor-incidents.md).
H2: "Soft caps — WARN by default, opt-in downgrade"
                                         (soft_pct semantics; the
                                          allow_downgrade annotation;
                                          spec §9 R10 anti-thrash).
H2: "Pricing refresh"                    (quarterly + ad-hoc-on-diff;
                                          commit-pinned URL cite;
                                          step-by-step refresh; verbatim
                                          from spec §3.8 line 426).
H2: "OTel cardinality"                   (OTEL_TRACES_SAMPLER recommendation;
                                          W6 spec §9 R6 cite; link to
                                          ./cost-governor-dashboards.md).
H2: "Cost API vs Usage API fallback"     (spec §3.4 line 207-218 verbatim;
                                          obs.EventCostReconcileFallback;
                                          pricing-twice limitation cite).
H2: "Admin API key handling"             (env-var-only; key never logged;
                                          sha256 fingerprint; spec §9 R15;
                                          link to incidents runbook for
                                          rotation procedure).
H2: "Substrate shadow phase"             (substrate spec §3 Phase B cite +
                                          divergence threshold; OR file a
                                          followup if substrate is already
                                          Phase C at impl time).
H2: "Example config"                     (cost: block from examples/full
                                          regatta.yaml — three shapes).
H2: "What is intentionally missing"      (spec §2 OOS — per-tenant, Stripe,
                                          forecasting, auto-downgrade
                                          default, real-time deny;
                                          `gh issue list --label
                                          cost-governor-followup` cite).
H2: "Where to look next"                 (links to T6 incidents + T7
                                          dashboards + spec for engineers).

# Test file — docs/operator/cost_governor_test.go

Copy docs/operator/observability_test.go verbatim, then:
- Rename `observabilityEnvVars` to `costGovernorConfigFields`.
- Populate with the 8 fields from spec §3.6 #CostGovernor:
  per_dag_usd, per_operator_usd, per_work_item_usd, period, soft_pct,
  reconcile_interval, drift_alert_threshold_pct, usage_api_key_env.
- Add tests (5 per spec §6 T5):
  TestCostGovernorDoc_LinksValid                          — same regex as observability test; allowlist forward-refs to in-flight T6/T7 paths via a `linkAllowlist` var the test skips.
  TestCostGovernorDoc_DocumentsAllConfigFields            — every #CostGovernor field name appears.
  TestCostGovernorDoc_PricingRefreshRunbookExists         — H2 "Pricing refresh" present + "https://www.anthropic.com/pricing" or "anthropic.com" URL appears.
  TestCostGovernorDoc_OTelCardinalityGuidanceExists       — H2 "OTel cardinality" present + "OTEL_TRACES_SAMPLER" appears + "W6" or "observability.md" cite.
  TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins — body contains "most-restrictive-wins" verbatim (case-insensitive).

# examples/full/regatta.yaml append

Locate the existing `safety:` block (or `safety: {}` line) and append a
commented-out cost: block UNDER it. Every line starts with `#`. Include:

  # cost:
  #   per_dag_usd: 100
  #   per_operator_usd: 50
  #   period: 1d
  #   soft_pct: 80
  #   reconcile_interval: 1h
  #   drift_alert_threshold_pct: 10
  #   usage_api_key_env: ANTHROPIC_ADMIN_KEY

Field values MUST validate under regatta.v1.cue when uncommented
(per spec §3.6 schema). DO NOT modify any other key in the file.

# Workflow

1. Write docs/operator/cost_governor_test.go FIRST. Run `go test ./docs/operator/...` — capture failing output (doc-file-missing errors are acceptable as failing-test capture per feedback_tdd_discipline; paste into PR body).
2. Write docs/operator/cost-governor.md to satisfy each test in turn.
3. Append cost: block to examples/full/regatta.yaml.
4. Run `make doc-check` + `make check` — both clean.
5. Run `go test ./docs/operator/...` — all 5 tests green.
6. (A+ rubric step) Spawn ONE reviewer subagent with prompt "You are a new regatta operator with no prior knowledge of cost-governor. Read docs/operator/cost-governor.md for ≤ 5 minutes. Answer three questions: (1) how do I set a per-DAG cap of $100? (2) what fires if I exceed a soft cap? (3) where do I rotate the Anthropic admin key? For each, cite the line number you used." Paste transcript into PR body.
7. File ≥ 4 [cost-governor-followup] issues per the PR body skeleton — confirm wedge total ≥ 13 (`gh issue list --label cost-governor-followup | wc -l`).
8. Push branch; open PR via `gh pr create --base main --body-file <path>`.
9. Spawn ONE adversarial reviewer subagent (per feedback_adversarial_review) with hunt list:
   - Every #CostGovernor field is documented (not just listed in the CUE block).
   - "Most-restrictive-wins" line is BOLDED, not just italicised.
   - Pricing-refresh section names the cadence ("quarterly").
   - OTel-cardinality section names BOTH OTEL_TRACES_SAMPLER and W6 spec §9 R6.
   - Forward-links to T6 + T7 use the allowlist mechanism if those PRs are not merged yet.
   - Substrate shadow-phase section exists OR a followup issue is filed citing why.
   - examples/full/regatta.yaml block validates when uncommented (manual check; or test gate if `TestConfigExamples_AllValidate` exists).
   - No AI signatures anywhere (feedback_no_signatures).
   - Comments discipline (feedback_comments_discipline) — test godocs ≤ 1 line.
10. Apply reviewer findings inline OR file tracking issue + cite in PR body per feedback_unaddressed_load_bearing.
11. Re-run `make check`. Force-push.
12. Flip automerge ONLY after reviewer cleared + CI green (per feedback_review_before_automerge).

# Hygiene

- NO AI signatures anywhere per feedback_no_signatures.
- Comments discipline per feedback_comments_discipline.
- All paths above are EXACT — no implementer renaming.

# Return format

- PR URL.
- 5-minute usability read transcript (A+-T5-1 evidence).
- 4 followup issue numbers filed.
- `gh issue list --label cost-governor-followup | wc -l` count (must be ≥ 13).
- Adversarial reviewer verdict (APPROVE or full findings).
- One-line diff stat (files changed; LoC added/removed).

Begin now. NEVER pause for user input.
```

---

## §3 Task T6 — Ops playbook `docs/engineer/runbooks/cost-governor-incidents.md`

### Scope

- **`docs/engineer/runbooks/cost-governor-incidents.md`** (NEW; ~200 lines; output path EXACTLY `docs/engineer/runbooks/cost-governor-incidents.md`; section anchor `#cost-governor-incidents` for the H1).
- **`docs/engineer/runbooks/cost_governor_incidents_test.go`** (NEW; tests-only Go package — `package runbooks`; gates link validity + every load-bearing `obs.EventCost*` slog event symbol is cited + every spec §9 R-tier section with operator-runbook content has a matching heading).
- Content outline (one H2 per incident; each section has fixed sub-structure: **Trigger** / **Symptoms** / **First-check** / **Diagnose** / **Recovery** / **Rollback** / **Spec cite**):
  1. **H1** "Cost-governor incident playbook" — reader-orientation paragraph (reader = on-call engineer or operator responding to a cost-gov alert). Read-time: 8 minutes scan; per-incident sections are 1-2 minute reads each.
  2. **H2 "EventCostReconcileFailing fires"** — covers spec §3.4 failure-mode table line 247-248 (429 persistent + 5xx persistent + network down). Trigger: ≥ 5 consecutive tick failures with the same upstream class. First-check: check Anthropic status page; check admin-key validity (use `grep -r "regatta\.cost\.api_key_fingerprint" <log>` — value will be a sha256 prefix, never the raw key); check rate-limit budget. Recovery: wait out the backoff (1s × 2^n capped 5min); if persistent > 4h, temporarily raise `safety.cost.drift_alert_threshold_pct` to suppress noise (cite spec §9 R3 mitigation verbatim) + file an Anthropic support ticket. Rollback: NONE — reconciler keeps trying; pre-call deny gate continues against the last successful `budget_reconciled` row per Fold semantics (cite spec §9 R6).
  3. **H2 "EventCostDriftAlert fires"** — Trigger: drift_pct > threshold. Symptoms: `actual_usd > recorded_usd` for a bucket. Diagnose flowchart: (a) check `obs.EventCostReconcileFallback reason=cost_api_unavailable` — if WARN-logged recently, the Usage-API path applied pricing locally; pricing table may be stale → run T5 pricing-refresh procedure. (b) Check `regatta cost.spend_unknown` slog events in the bucket window (cite spec §9 R13 — spawner SIGKILL'd mid-call without emitting `result`). (c) Check for a substrate write-skew event (spec §9 R4 — `llm_call` span left open with `error.type=record_call_failed`). Recovery: usually self-healing via LWW on the next Cost API success; if persistent, run the backfill recipe (`regatta cost backfill --since 24h` — see followup-CLI section). Rollback: drift signal is informational; never auto-correct (cite spec §3.4 line 240).
  4. **H2 "EventCostReconcileSkipped fires"** — Trigger: admin key env unset at boot. First-check: confirm `safety.cost.usage_api_key_env` env-var name + that the env var is exported in the regatta process environment. Recovery: set the env var per the operator runbook §"Admin API key handling"; restart `regatta serve`; reconciler resumes on next tick. Rollback: pre-call deny gate continues against recorded spend; degraded but not broken (cite spec §3.4 line 246).
  5. **H2 "EventCostSoftCapBreached fires"** — Trigger: spend_pct > soft_pct (default 80). Default action: WARN-only (no model swap). If operator has set `work_item.annotations.cost.allow_downgrade: true`, the spawner SHOULD pass `Verdict.DowngradeTo` to the LLM. Diagnose: check the work_item's annotation map; if missing, the WARN is by-design. Recovery: NONE if WARN-only is the desired behaviour (the cap is firm; spawn proceeds; soft-cap is a heads-up). If downgrade is desired, set the annotation per spec §9 R10. Rollback: ratchet rule — once soft-cap fires for a (scope, period) tuple, the period STAYS in soft-cap state until period rolls (cite spec §9 R10).
  6. **H2 "Anthropic admin key rotation procedure"** — step-by-step: (a) generate the new admin key in Anthropic console; (b) set the new env-var value in the operator's secret-store (1Password / k8s secret / systemd-credentials — list cited per spec §9 R15 followup); (c) rolling-restart regatta `serve` processes one at a time (no in-process rotation in MVP-2 — closes the `[cost-governor-followup] admin-key-vault integration` issue); (d) confirm the next reconcile tick succeeds (sha256 fingerprint in slog WARN-on-rotation line will be the new fingerprint); (e) revoke the old key in Anthropic console AFTER ≥ 2 successful ticks on the new key. Pin: regatta NEVER caches or logs the raw key.
  7. **H2 "Pricing-table rollback"** — step-by-step: (a) `git revert` the pricing PR that introduced the bad row; (b) bump `pricing_rev` constant DOWN one (or to a fresh higher value documenting the revert); (c) emergency PR with `[cost-governor-rollback]` tag; (d) the next reconcile tick will use the rolled-back table; (e) any `budget_reconciled` rows written during the bad-pricing window are NOT auto-corrected — LWW means the next clean tick supersedes; (f) optional: run `regatta cost backfill --since <bad-pricing-window>` if the followup CLI has shipped. Pin: rollback is a code change + redeploy; substrate rows are append-only and self-correct on next reconcile (cite spec §7 B3 + spec §3.5 reducer semantics).
  8. **H2 "Spawner SIGKILL drift recovery (R13)"** — covers spec §9 R13 mitigation. Trigger: persistent drift_pct > 0 with no obvious failed-call alert. Diagnose: check for `regatta.cost.spend_unknown` slog events in the bucket window (these fire if the followup `spawner reconciliation outbox` ships; if it doesn't, the diagnose-step is "check the spawner process logs for SIGKILL in the affected bucket"). Recovery: the drift signal IS the recovery — Anthropic billed for the lost call; regatta now knows about it via the budget_reconciled row's `actual_usd > recorded_usd`. The pre-call deny gate already counts the drift (it reads `actual_usd` not `recorded_usd` when Cost API path is fresh — verify spec §3.4 semantics) so future caps reflect the true spend. Rollback: NONE.
  9. **H2 "Where to find config"** — pointer to `docs/operator/cost-governor.md` §"Config surface"; pointer to the spec for engineer-level references.
  10. **H2 "What this incident affects"** — table mapping each incident type to user-visible impact (pre-call deny continues / soft-cap WARN continues / reconciler degraded / drift signal degraded). Closes the "is this a customer-impacting outage?" triage question.

### Prereqs (cite spec sections)

- Spec §3.4 — reconciliation failure-mode table (verbatim source for each H2 above).
- Spec §3.5 — substrate reducer semantics (LWW for budget_reconciled informs the "self-correcting" framing).
- Spec §9 R3 (rate limit), R4 (write-skew), R6 (Anthropic down), R10 (soft-cap thrash), R13 (SIGKILL drift), R15 (admin key) — every H2 cites at least one R-tier section.
- Spec §10 Wave 3 — runbook is in-scope per the wave-3 doc-set framing.

### Existing patterns to reuse (do NOT reinvent)

- **`docs/operator/observability.md`** — voice register + link-validity test pattern.
- **`docs/operator/observability_test.go`** — link-checker test template; T6 copies the regex + readDoc helper.
- **SRE runbook idioms** (cited per `feedback_research_design_principles`) — Datadog "Runbooks for cost anomalies" + AWS Budgets "Resolve a billing alarm" + Google SRE Workbook "On-call playbook" template. Each section has Trigger / Symptoms / First-check / Diagnose / Recovery / Rollback / Spec-cite — copy this structure 1:1.
- **NO new test infra.** T6's `_test.go` is structurally identical to `observability_test.go`.

### B/A/A+ mini-rubric for T6

- **B (T6 floor):** all 3 test gates green; `make doc-check` clean; every R-tier section cited has a matching H2; doc renders via `glow`.
- **A (T6 target):** B + every H2 has the full 7-line sub-structure (Trigger/Symptoms/First-check/Diagnose/Recovery/Rollback/Spec-cite); adversarial reviewer verdict APPROVE.
- **A+ (T6 stretch, OPTIONAL):** doc passes a **3-minute on-call simulation** — reviewer subagent simulates an on-call engineer woken at 02:00 by `EventCostReconcileFailing reason=rate_limited` and asks "what do I do in the next 3 minutes?" The reviewer's answer must cite the correct H2 + the first-check step. PR body contains the transcript.

### Tests (3 named — gate the doc against drift)

1. `TestCostGovernorIncidentsRunbook_LinksValid` — every relative `.md` link resolves on disk (same regex as observability test). Forward-link to `../../operator/cost-governor.md` MUST resolve (T5 ships in parallel — uses the linkAllowlist mechanism described in §1 if T5 isn't merged yet).
2. `TestCostGovernorIncidentsRunbook_CitesAllEventNames` — every `obs.EventCost*` symbol exported by Wave-1/2 code (EventCostReconcileFailing, EventCostReconcileSkipped, EventCostReconcileFallback, EventCostDriftAlert, EventCostSoftCapBreached) appears in the runbook body. Failing the test means the runbook missed an event class.
3. `TestCostGovernorIncidentsRunbook_CitesAllRiskSections` — every R-tier spec section that has operator-runbook content (R3, R4, R6, R10, R13, R15 per the spec's "Operator runbook documents…" hints) is named in the runbook body via the verbatim "R3" / "R13" style cite.

### PR body skeleton — T6

````
## Summary

Cost-governor Wave 3 T6 (optional spec extension) ships the ops
incident playbook for spec §9 R3 / R4 / R6 / R10 / R13 / R15 alarms.

- docs/engineer/runbooks/cost-governor-incidents.md — 200-line on-call
  runbook covering EventCostReconcileFailing, EventCostDriftAlert,
  EventCostReconcileSkipped, EventCostSoftCapBreached + admin-key
  rotation + pricing-table rollback + SIGKILL drift recovery.
- docs/engineer/runbooks/cost_governor_incidents_test.go — gates link
  validity + every obs.EventCost* event cited + every R-tier section
  with runbook content cited.

This PR establishes the docs/engineer/runbooks/ directory as a new
location for engineer-facing SRE runbooks (previously absent — verified
empty at plan time).

## Why

Per spec §10 Wave 3 (extension): the operator doc (T5) is the
quickstart; the incidents playbook is the on-call surface. Without T6,
on-call engineers fall back to reading the spec — which is engineering
reference, not runbook material.

## Test plan

- [x] TestCostGovernorIncidentsRunbook_LinksValid
- [x] TestCostGovernorIncidentsRunbook_CitesAllEventNames
- [x] TestCostGovernorIncidentsRunbook_CitesAllRiskSections
- [x] make doc-check clean
- [x] make check clean

## Deletion default

T6 ELIMINATES the on-call's need to read the spec §9 R-tier table to
respond to an alarm — the playbook owns the runbook surface and the
spec stays an engineering reference. One doc replaces "read the spec"
implicit guidance.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] in-process admin-key rotation without restart (#NNN; spec §9 R15)
- [cost-governor-followup] regatta cost backfill --since <window> CLI (#NNN; spec §9 R6 + R13)

```release-notes
[FEATURE] cost-governor incident playbook docs/engineer/runbooks/cost-governor-incidents.md (on-call reference; no behaviour change)
```
````

### Dispatch prompt — T6 (paste-ready)

```
You are an implementer subagent on worktree
.claude/worktrees/agent-cost-gov-t6. You ship ONE PR.

# Output paths (exact — no implementer choice per feedback_plan_subagent_dup_files)

- docs/engineer/runbooks/cost-governor-incidents.md       (NEW; ~200 lines; H1 section anchor #cost-governor-incidents)
- docs/engineer/runbooks/cost_governor_incidents_test.go  (NEW; tests-only Go; package runbooks)

You MUST NOT write to any other path. Specifically:
- Do NOT touch docs/operator/ (T5 + T7 exclusive scope).
- Do NOT touch internal/ or cmd/ or contracts/.

# Spec authority

Source-of-truth: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read §3.4 (reconciliation failure-mode table verbatim), §3.5 (substrate
LWW semantics), §9 R3 + R4 + R6 + R10 + R13 + R15. Every H2 in the
runbook cites at least one of these sections.

# Reference systems to mirror (per feedback_research_design_principles)

- Datadog "Runbooks for cost anomalies"  — section rhythm.
- AWS Budgets "Resolve a billing alarm"  — incident-trigger framing.
- Google SRE Workbook on-call playbook   — Trigger/Symptoms/First-check/
                                            Diagnose/Recovery/Rollback/
                                            Spec-cite 7-line structure.
- docs/operator/observability_test.go    — test-file template.

# Content requirements (one H2 per incident — exact list)

H1: "Cost-governor incident playbook"  + reader/read-time paragraph.

H2 (each with the 7-line sub-structure):
  "EventCostReconcileFailing fires"           — spec §3.4 + R3 + R6.
  "EventCostDriftAlert fires"                 — spec §3.4 + R4 + R13.
  "EventCostReconcileSkipped fires"           — spec §3.4 line 246 + R15.
  "EventCostSoftCapBreached fires"            — spec §9 R10.
  "Anthropic admin key rotation procedure"    — spec §9 R15.
  "Pricing-table rollback"                    — spec §3.8 + §7 B3.
  "Spawner SIGKILL drift recovery (R13)"      — spec §9 R13.
  "Where to find config"                      — link to ../../operator/cost-governor.md.
  "What this incident affects"                — impact table.

# Test file — docs/engineer/runbooks/cost_governor_incidents_test.go

Copy docs/operator/observability_test.go pattern; package `runbooks`.
Add 3 tests:

  TestCostGovernorIncidentsRunbook_LinksValid       — relative .md link resolver; allowlist forward-refs to T5/T7 with linkAllowlist if they're in-flight.
  TestCostGovernorIncidentsRunbook_CitesAllEventNames — every symbol below appears verbatim in the doc body:
                                                       EventCostReconcileFailing, EventCostReconcileSkipped,
                                                       EventCostReconcileFallback, EventCostDriftAlert,
                                                       EventCostSoftCapBreached.
  TestCostGovernorIncidentsRunbook_CitesAllRiskSections — every of R3, R4, R6, R10, R13, R15 cited in the doc body (look for "spec §9 R3" or just "R3" patterns).

# Workflow

1. Write the test file first; run `go test ./docs/engineer/runbooks/...` — capture failing output.
2. Write the runbook to satisfy each test.
3. Run `make doc-check`, `make check`, `go test ./docs/engineer/runbooks/...` — all clean.
4. File 2 followup tracking issues per PR body skeleton.
5. Push branch; open PR via `gh pr create --base main --body-file <path>`.
6. Main session reviews inline (no separate adversarial subagent for T6 per feedback_review_proportional — docs-only optional extension).
7. Flip automerge after CI green.

# Hygiene

- NO AI signatures (feedback_no_signatures).
- Comments discipline (feedback_comments_discipline).

# Return format

- PR URL.
- 2 followup issue numbers.
- One-line diff stat.

Begin now. NEVER pause for user input.
```

---

## §4 Task T7 — Dashboard cite `docs/operator/cost-governor-dashboards.md`

### Scope

- **`docs/operator/cost-governor-dashboards.md`** (NEW; ~150 lines; output path EXACTLY `docs/operator/cost-governor-dashboards.md`; section anchor `#cost-governor-dashboards` for the H1).
- **`docs/operator/cost_governor_dashboards_test.go`** (NEW; tests-only Go package — `package operator`, same package as T5's test file. Different filename so no collision; same package-level helpers reusable if T5's test file is already merged. If T5 lands first, T7's `_test.go` MAY import T5's helpers; if T7 lands first or in parallel, T7 ships its own helper copy — minimal duplication acceptable per `feedback_review_proportional`).
- Content outline:
  1. **H1** "Cost-governor dashboards" — Reader = operator wiring regatta cost spans + slog events into their observability backend (Honeycomb / Grafana / Jaeger / Datadog). Read-time: 5 minutes. Pointer to T5 ("Read the operator runbook first") for cost-gov semantics.
  2. **H2 "Span attributes mapped to panels"** — table:

      | Span attr | Span | Panel type | Honeycomb | Grafana (Tempo TraceQL) | Jaeger query |
      | --- | --- | --- | --- | --- | --- |
      | `regatta.cost.usd_estimate` | `cost.evaluate` | scatter | `VISUALIZE: HEATMAP(regatta.cost.usd_estimate) WHERE name = "cost.evaluate"` | `{name="cost.evaluate"} \| select(span.regatta.cost.usd_estimate)` | tags: `name=cost.evaluate` |
      | `regatta.cost.cap_dag_usd` | `cost.evaluate` | distribution | `VISUALIZE: P95(regatta.cost.cap_dag_usd)` | `{name="cost.evaluate"} \| select(span.regatta.cost.cap_dag_usd)` | tags: `regatta.cost.cap_dag_usd != 0` |
      | `regatta.cost.cap_op_usd` | `cost.evaluate` | distribution | same shape | same shape | same shape |
      | `regatta.cost.allow` | `cost.evaluate` | rate | `VISUALIZE: RATE_AVG WHERE regatta.cost.allow = false` | `{name="cost.evaluate" && span.regatta.cost.allow = false}` | tags: `regatta.cost.allow=false` |
      | `regatta.cost.soft_breached` | `cost.evaluate` | rate | same shape | same shape | same shape |
      | `regatta.cost.period_start` | `cost.reconcile` | timeline | `WHERE name = "cost.reconcile" GROUP BY regatta.cost.period_start` | `{name="cost.reconcile"}` | tags: `name=cost.reconcile` |
      | `regatta.cost.drift_pct` | `cost.reconcile` | scatter + threshold | `VISUALIZE: MAX(regatta.cost.drift_pct)` overlay drift_alert_threshold_pct | `{name="cost.reconcile"} \| select(span.regatta.cost.drift_pct > 10)` | tags: `regatta.cost.drift_pct>10` |
      | `regatta.cost.api_source` | `cost.reconcile` | rate by attr-value | `WHERE name = "cost.reconcile" GROUP BY regatta.cost.api_source` | `{name="cost.reconcile"}` | tags: `regatta.cost.api_source=usage_fallback` |

  3. **H2 "Slog events mapped to alerts"** — table:

      | slog event | severity | Recommended alert | Panel cite |
      | --- | --- | --- | --- |
      | `obs.EventCostReconcileFailing` | ERROR | page on-call after 4h continuous | drift-pct timeline gap |
      | `obs.EventCostReconcileSkipped` | WARN | ticket if env-var ever unset in prod | api_source rate |
      | `obs.EventCostReconcileFallback` | WARN | trend-line; spike means Cost API outage | api_source = `usage_fallback` rate |
      | `obs.EventCostDriftAlert` | WARN | page on `drift_pct > 25` for sustained period | drift-pct scatter |
      | `obs.EventCostSoftCapBreached` | INFO | dashboard tile; never page | soft_breached rate |

  4. **H2 "Suggested dashboard layout"** — 4-tile recommendation: (a) "Spend rate" — `regatta.cost.usd_estimate` heatmap on `cost.evaluate`; (b) "Cap denials" — `regatta.cost.allow = false` rate on `cost.evaluate`; (c) "Drift" — `regatta.cost.drift_pct` scatter on `cost.reconcile`; (d) "Reconciler health" — `EventCostReconcileFailing` rate. Each tile cites the underlying attr/event verbatim.
  5. **H2 "Sampling and cost dashboards"** — short cite to `docs/operator/observability.md#sampler-customization`. `cost.evaluate` cardinality is bounded by `lane_cap × num_lanes`; sampler at 0.01 is fine for high-tick-rate deployments. Reconciler spans are 1/hour by default — never sample. Pin spec §9 R14.
  6. **H2 "Cross-references"** — links to T5 (operator runbook), T6 (incident playbook), W6 observability doc.

### Prereqs (cite spec sections)

- Spec §3.4 — reconciler emits `cost.reconcile` span attrs.
- Spec §3.7 — OTel attr table verbatim (4 cost attrs + 3 regatta-scope attrs + period/drift/api_source on the reconciler span).
- Spec §7 A+4 — Honeycomb / Grafana / Jaeger query examples (this task IS the A+4 deliverable). PR body cites this.
- Spec §9 R14 — cardinality bound.

### Existing patterns to reuse (do NOT reinvent)

- **`docs/operator/observability.md`** §"Cardinality" + §"Sampler customization" — adjacent doc; T7 links there for sampler config rather than repeating.
- **W6 spec §4.1 span hierarchy** — anchor for "where do these spans live".
- **Honeycomb + Tempo TraceQL + Jaeger query syntax** (cited per `feedback_research_design_principles`) — each backend has a published query reference; T7 cites the published syntax. T7 does NOT invent query DSL.

### B/A/A+ mini-rubric for T7

- **B (T7 floor):** all 3 test gates green; `make doc-check` clean.
- **A (T7 target):** B + every `regatta.cost.*` attr from spec §3.7 appears in the "Span attributes" table; every `obs.EventCost*` event from Wave 2 appears in the "Slog events" table; adversarial reviewer verdict APPROVE.
- **A+ (T7 stretch, OPTIONAL):** PR body includes one screenshot per backend (Honeycomb + Grafana + Jaeger) showing the rendered dashboard panel — pins spec §7 A+4 verbatim ("PR body includes one screenshot per backend"). The author runs the dashboard against a synthetic Wave-2 test stub (acceptable per spec — synthesised data is fine for the screenshot).

### Tests (3 named — gate the doc against drift)

1. `TestCostGovernorDashboardsDoc_LinksValid` — every relative `.md` link resolves on disk. Same regex as observability test. Forward-link to T5 + T6 uses the linkAllowlist mechanism if those are in-flight.
2. `TestCostGovernorDashboardsDoc_CitesAllCostSpanAttrs` — every `regatta.cost.*` attribute named in spec §3.7 appears in the doc body. Pinned list:
   - `regatta.cost.usd_estimate`
   - `regatta.cost.cap_dag_usd`
   - `regatta.cost.cap_op_usd`
   - `regatta.cost.allow`
   - `regatta.cost.soft_breached`
   - `regatta.cost.period_start`
   - `regatta.cost.period_end`
   - `regatta.cost.drift_pct`
   - `regatta.cost.api_source`
3. `TestCostGovernorDashboardsDoc_CitesAllSlogEvents` — every `obs.EventCost*` symbol exported by Wave-1/2 code appears in the slog-events table (same list as T6's `TestCostGovernorIncidentsRunbook_CitesAllEventNames`).

### PR body skeleton — T7

````
## Summary

Cost-governor Wave 3 T7 (optional spec extension; closes spec §7 A+4)
ships the dashboard cite mapping regatta.cost.* span attrs +
obs.EventCost* slog events to Honeycomb / Grafana / Jaeger queries.

- docs/operator/cost-governor-dashboards.md — 150-line panel-by-panel
  table mapping every cost.evaluate + cost.reconcile span attribute to
  ready-to-paste queries for the three reference backends, plus the
  slog-event-to-alert mapping and the suggested 4-tile dashboard
  layout.
- docs/operator/cost_governor_dashboards_test.go — gates link validity +
  every regatta.cost.* attr from spec §3.7 cited + every obs.EventCost*
  event cited.

## Why

Per spec §7 A+4: "Dashboard query examples in operator doc include
verified Honeycomb + Grafana + Jaeger queries that drop into the
operator's existing observability stack." T7 IS that deliverable.
Operators eliminate the per-team "what query should I use?" Slack
thread.

## Test plan

- [x] TestCostGovernorDashboardsDoc_LinksValid
- [x] TestCostGovernorDashboardsDoc_CitesAllCostSpanAttrs
- [x] TestCostGovernorDashboardsDoc_CitesAllSlogEvents
- [x] make doc-check clean
- [x] make check clean
- [x] (A+ optional) 3 screenshots (Honeycomb + Grafana + Jaeger) inline below.

## Deletion default

T7 ELIMINATES the per-operator "what query should I use?" Slack thread
that recurs for every backend. One table replaces three documents-worth
of inline support.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] Datadog query example parity (#NNN; spec §7 A+4 hints "Datadog" without committing — defer to v2)

```release-notes
[FEATURE] cost-governor dashboards cite docs/operator/cost-governor-dashboards.md (no behaviour change; closes spec §7 A+4)
```
````

### Dispatch prompt — T7 (paste-ready)

```
You are an implementer subagent on worktree
.claude/worktrees/agent-cost-gov-t7. You ship ONE PR.

# Output paths (exact — no implementer choice per feedback_plan_subagent_dup_files)

- docs/operator/cost-governor-dashboards.md           (NEW; ~150 lines; H1 anchor #cost-governor-dashboards)
- docs/operator/cost_governor_dashboards_test.go      (NEW; tests-only Go; package operator)

You MUST NOT write to any other path. Specifically:
- Do NOT touch docs/operator/cost-governor.md (T5's exclusive scope).
- Do NOT touch docs/engineer/runbooks/ (T6's exclusive scope).
- Do NOT touch internal/ or cmd/ or contracts/.

# Spec authority

Source-of-truth: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read §3.4 (reconciler span attrs), §3.7 (OTel attr table verbatim), §7
A+4 (this task IS the deliverable), §9 R14 (cardinality).

# Reference systems to mirror (per feedback_research_design_principles)

- docs/operator/observability.md §"Cardinality" + §"Sampler customization" — adjacent doc; link rather than repeat.
- Honeycomb + Tempo TraceQL + Jaeger published query syntax — cite the documented form; do NOT invent DSL.
- W6 spec §4.1 span hierarchy — anchor for span placement.

# Content requirements

H1: "Cost-governor dashboards" + reader/read-time + forward-link to ./cost-governor.md ("Read the operator runbook first").

H2: "Span attributes mapped to panels" — markdown table with columns
    [Span attr, Span, Panel type, Honeycomb, Grafana (Tempo TraceQL),
     Jaeger query]. Every regatta.cost.* attr from spec §3.7 appears.
    Pinned attribute list (the test gate enforces verbatim presence):
    regatta.cost.usd_estimate, regatta.cost.cap_dag_usd,
    regatta.cost.cap_op_usd, regatta.cost.allow,
    regatta.cost.soft_breached, regatta.cost.period_start,
    regatta.cost.period_end, regatta.cost.drift_pct,
    regatta.cost.api_source.

H2: "Slog events mapped to alerts" — markdown table with columns
    [slog event, severity, Recommended alert, Panel cite]. Every
    obs.EventCost* symbol from Wave-1/2 appears (5 events:
    EventCostReconcileFailing, EventCostReconcileSkipped,
    EventCostReconcileFallback, EventCostDriftAlert,
    EventCostSoftCapBreached).

H2: "Suggested dashboard layout" — 4-tile recommendation; each tile
    cites the underlying attr/event verbatim.

H2: "Sampling and cost dashboards" — link to
    ../operator/observability.md#sampler-customization (sibling — same
    directory; resolve as ./observability.md#sampler-customization).
    Cite spec §9 R14 cardinality bound.

H2: "Cross-references" — links to ./cost-governor.md (T5),
    ../engineer/runbooks/cost-governor-incidents.md (T6),
    ./observability.md (W6).

# Test file — docs/operator/cost_governor_dashboards_test.go

Package `operator`. Copy docs/operator/observability_test.go pattern.

  TestCostGovernorDashboardsDoc_LinksValid          — relative-link resolver; allowlist forward-refs to T5/T6.
  TestCostGovernorDashboardsDoc_CitesAllCostSpanAttrs — pinned 9-attribute list (above) appears verbatim.
  TestCostGovernorDashboardsDoc_CitesAllSlogEvents     — pinned 5-event list (above) appears verbatim.

# Workflow

1. Write test file first; run `go test ./docs/operator/...` — capture failing output.
2. Write the dashboards doc to satisfy each test.
3. Run `make doc-check`, `make check`, `go test ./docs/operator/...` — all clean.
4. (A+ optional) Render dashboards against a synthetic Wave-2 stub; screenshot Honeycomb + Grafana + Jaeger panels; embed in PR body.
5. File the 1 followup tracking issue per PR body skeleton.
6. Push branch; open PR via `gh pr create --base main --body-file <path>`.
7. Main session reviews inline (no separate adversarial subagent per feedback_review_proportional).
8. Flip automerge after CI green.

# Hygiene

- NO AI signatures (feedback_no_signatures).
- Comments discipline (feedback_comments_discipline).

# Return format

- PR URL.
- 1 followup issue number.
- (A+ optional) 3 screenshot links.
- One-line diff stat.

Begin now. NEVER pause for user input.
```

---

## §5 Followup templates (pre-enumerated per `feedback_parallel_dup_followups`)

The Wave 3 PRs collectively close the cost-gov wedge. Per spec §7 A7 the wedge MUST have ≥ 13 `[cost-governor-followup]` issues filed at close. Wave 1 + Wave 2 PRs filed ~10; Wave 3 fills the remainder. Templates below are PRE-FILED by the main session before Wave 3 dispatches so implementers cite existing numbers rather than duplicate. Per `feedback_parallel_dup_followups`, the main thread files these before fan-out.

**Pre-filed by main session before Wave 3 dispatch** (cite the assigned `#NNN` in each dispatch prompt):

1. `[cost-governor-followup] regatta cost backfill --since <window> CLI` — spec §9 R6 + R13. T5 + T6 both want to cite this CLI when describing drift-recovery and Anthropic-down scenarios.
2. `[cost-governor-followup] auto-downgrade per-tenant opt-in surface` — spec §9 R10. The `work_item.annotations.cost.allow_downgrade` annotation needs a per-tenant default in W8 RBAC; tracking issue defers the design.
3. `[cost-governor-followup] cost.evaluate sampler integration test` — spec §9 R14. The cardinality bound is asserted at spec time; a CI test that runs `regatta serve` at high tick-rate + asserts `cost.evaluate` span count stays under `lane_cap × num_lanes × ticks` is a deferred runtime guard.
4. `[cost-governor-followup] examples/full/regatta.yaml validate-config gate` — A+-T5-2. If `TestConfigExamples_AllValidate` doesn't already exist, file it.
5. `[cost-governor-followup] in-process admin-key rotation without restart` — spec §9 R15. Current procedure (T6 H2 "rotation") requires rolling restart; in-process SIGHUP-style rotation is a follow-on.
6. `[cost-governor-followup] Datadog query example parity` — spec §7 A+4 hints at Datadog without committing in the spec; T7 ships Honeycomb + Grafana + Jaeger only.

**Wedge-close A7 verification step** (run by the last Wave 3 PR to merge): `gh issue list --label cost-governor-followup | wc -l` ≥ 13. If under, the main session files additional issues from spec §2 OOS to reach the bar (per-tenant + per-team budgets, Stripe webhook, predictive forecasting, mid-DAG kill+compensation, cache-aware budgeting, cross-fleet MCP attribution, Bedrock pricing, Pricing API auto-flag, history estimator opt-in S1, pricing_override_path config surface S2, admin-key-vault integration R15, spawner reconciliation outbox R13, progress-gated renewal).

---

## §6 After Wave 3 — wedge close

Wave 3 exit gate (per spec §10 line 776):
- `regatta serve` with `safety.cost.per_dag_usd: 100` set denies the over-cap spawn at the scheduler tick + emits the `cost.evaluate` span + writes the `token_spend` substrate row on the allowed call.
- All three Wave 3 docs pass `make doc-check`.
- Every config field documented in T5.
- Cardinality recommendation in T5 + T7.
- Cost-vs-Usage-API fallback semantics documented in T5 (and visible in T7 via `regatta.cost.api_source` row).
- Most-restrictive-wins precedence bolded in T5.
- ≥ 13 `[cost-governor-followup]` issues filed (A7 verified).

**Boot-prompt refresh (`feedback_boot_prompt_per_wave_refresh`):** after Wave 3 merges, refresh `docs/engineer/autonomous-session-prompt.md` to drop the cost-governor wedge from the active section + add a "shipped" line citing the three Wave 3 PR numbers. Doc-only; skip review.

**Roadmap pre-fetch (`feedback_roadmap_pre_fetch`):** while Wave 3 is in-flight, the main session pre-fetches the next wedge horizon (likely W7 operator UI's cost panel, which consumes the T5 runbook + T7 dashboard cites). No Wave-4 of cost-gov exists — the wedge closes here.

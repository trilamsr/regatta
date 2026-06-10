---
id: OBS-WAVE-A-T6
title: operator-facing metric-layer doc — env vars, exporter choice, dashboards, SLO index
lane: observability
status: planned
dependencies: OBS-WAVE-A-T5
linked_artifact: docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md` §6.4 (dashboards-as-code commitment), §7 Wave-A table row A-T6, §9 R4 (Prom-vs-OTLP wire choice), §10 dispatch brief A-T6.

## Task

Write `docs/operator/observability-metrics.md` — the operator-facing surface for the metric layer landed in A-T0a through A-T5. Target length ≤ 350 lines.

Sections (mandatory):

1. **Env-var contract** — `OTEL_EXPORTER_OTLP_ENDPOINT` (OTLP gRPC) vs `OTEL_METRICS_PROMETHEUS_PORT` (Prom pull). Document the `ErrOTelMetricExporterConflict` mutual-exclusion validator from A-T0a; both set is operator error.
2. **Prom-vs-OTLP wire choice** — when to pick each (spec §9 R4): Prom pull for self-host + Grafana; OTLP push for Honeycomb / Datadog / managed backends. Both transport the same metric set verbatim — no code change to swap.
3. **Dashboard provisioning** — `make provision-dashboards` calls the Grafana HTTP API to upsert every JSON file under `docs/operator/dashboards/`. Document the GRAFANA_URL + GRAFANA_API_TOKEN env vars; document the per-PR dashboard-JSON drift gate (`TestDashboardMetricNames_MatchEmitted`, spec §9 R2).
4. **SLO definitions + runbook index** — SLO-1 (scheduler tick) + SLO-2 (L4 latency); link to `docs/operator/runbooks/scheduler-tick.md` + `docs/operator/runbooks/l4-latency.md` (landed by A-T5). Document multi-burn-rate alert semantics + the Sloth version-pin contract (spec §9 R3).
5. **Cardinality budget + AST-walk lint** — quote spec §2.2 banned-tag list verbatim (`pr_number`, `run_id`, `work_item_id`); link to `TestMetricCardinality_PRNumberLabelBanned` lint shipped by A-T0a; document why log + trace correlation replaces high-card labels.
6. **Sample `/metrics` scrape** — paste the Prom-format scrape from A-T0a's PR body (per amendment §2 L2 reopen-condition) so operators can grep-verify the unit-suffix double-render (`regatta_scheduler_tick_latency_ms_milliseconds`) is the expected wire shape, not a bug.
7. **Trace head-sampling knobs** — quote spec §2.5: `OTEL_TRACES_SAMPLER_ARG` (default `0.1` = 10% head-sampling) + `OTEL_TRACES_SAMPLER=always_on` for debug + `OTEL_TRACES_SAMPLER=always_off` for emergency cost-stop. Note the always-on override for `error.type` + chain-verify + divergence-audit spans (i.e. real incidents ship at 100% even when steady-state sampling is 10%). Verify with the sampled-ratio measurement from A-T0a's PR body.

**Hard rules:**

- Pre-push grep against `feedback_doc_check_banned_phrases` (11 banned tokens — `blazing[- ]fast`, `production[- ]grade`, `world[- ]class`, `best[- ]in[- ]class`, `industry[- ]leading`, `cutting[- ]edge`, `lightning[- ]fast`, `battle[- ]tested`, `enterprise[- ]grade`, `rock[- ]solid`, `robust`). Reword every hit to falsifiable language.
- Per `feedback_research_design_principles`: every operator-facing claim cites a tool-check (CI test name or runbook step). No marketing prose; every paragraph answers "what does the operator do?"
- Per `feedback_comments_discipline`: WHY not WHAT. Drop sentences that recap what a section header already says.
- Cross-doc link phasing (`feedback_cross_doc_link_phasing`): links to A-T5's runbooks must resolve at A-T6 merge — sibling docs that cross-link fail doc-check per-PR. Either land A-T5 first (current dep ordering) OR phase-land with strip+restore.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; `scripts/doc-check.sh` clean (link integrity + banned-phrase gates); doc ≤ 350 lines; 7 sections present (env-vars / wire choice / dashboards / SLOs / cardinality / sample scrape / head-sampling knobs); B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer subagent clears; every claim cites a CI test name or runbook step (no unverified prose); sample `/metrics` scrape from A-T0a's PR body included verbatim (amendment §2 L2 reopen-condition closure).
- **A+ (stretch):** A + the doc is referenced from the top-level `docs/operator/observability.md` index file (no orphan); cross-doc link tests green (`feedback_cross_doc_link_phasing`); every section ends with a one-line "verify by running:" command the operator can copy-paste.

## Acceptance criteria

- [planned] c1: `docs/operator/observability-metrics.md` checked in with the 7 mandatory sections (incl. head-sampling knobs per spec §2.5); ≤ 350 lines (spec §6.4 + §10 dispatch brief A-T6).
- [planned] c2: Pre-push banned-phrase grep clean against all 11 tokens (`feedback_doc_check_banned_phrases`).
- [planned] c3: All relative .md links resolve to on-disk files; `scripts/doc-check.sh` exits 0 (`feedback_cross_doc_link_phasing`).
- [planned] c4: Sample `/metrics` scrape from A-T0a included verbatim (amendment §2 L2 reopen-condition closure).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence (category `[DOCS]`); submitted via `--body-file`.

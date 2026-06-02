---
id: OBS-WAVE-A-T0a
title: OTel MeterProvider + OTLP/Prometheus exporters + mutual-exclusion validator + AST-walk trap lint + Config.Meter retrofit for cost/spend + gates/l4
lane: observability
status: planned
dependencies:
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §2.4 (impl seam), §7 Wave-A table row A-T0a (post-amendment split), §10 dispatch brief A-T0, §11 cross-wedge contracts (W6 seam), §9 R4 (Prom-exporter wiring) + R8 (W8 hand-off).
Amendment ref: review of PR #410 §3 (A-T0a/A-T0b split) + §7 RISK-A (file-ownership fence — retrofit lives in `config.go`, NOT `writer.go` or gate-decide path).

## Task

Extend `internal/obs/otel/setup.go` to init an OTel `MeterProvider` alongside the existing `TracerProvider` (W6 #159). New file `internal/obs/otel/meter.go` exports the helpers — `Setup()` returns the provider and stores it on the global `otel.MeterProvider`. Nil meter on a `Config` falls back to `otel.Meter("<component>")` — same pattern as W6's `Config.Tracer` per `feedback_spec_pattern_authority`.

Wire two exporters with a mutual-exclusion validator:

- OTLP-metric exporter when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (uses `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`).
- Prometheus exporter when `OTEL_METRICS_PROMETHEUS_PORT` is set (uses `go.opentelemetry.io/otel/exporters/prometheus`).
- Both set simultaneously returns `ErrOTelMetricExporterConflict` — operator picks one wire format.

Retrofit `Config.Meter metric.Meter` field onto the TWO Config structs A-T1 + A-T2 need: `internal/cost/spend/config.go` (or wherever the existing Config lives) and `internal/gates/l4/config.go`. The remaining 6 Config retrofits live in A-T0b — do NOT touch them here.

**File-ownership fence (critical, per amendment review §7 RISK-A).** A-T0a's retrofit edits the Config struct file ONLY. Do NOT edit `internal/cost/spend/writer.go` (shared-owner pin: A-T1 owns it across Wave A + Wave C per `feedback_shared_primitive_owner`). Do NOT edit the L4 gate-decide path itself. If the existing Config sits in the same file as the writer/decide logic, extract the Config struct into a sibling `config.go` file first; that extraction is in-scope for A-T0a.

Ship the AST-walk lint test `TestMetricCardinality_PRNumberLabelBanned` in this PR (per §9 trap #1) — walks `meter.*Counter`/`*Histogram`/`*Gauge` call sites and fails on `pr_number`/`run_id`/`work_item_id` label literals.

Hand-off for R8: meter hardcodes `tenant_id=default` at this PR; W8 swaps for `ctx`-derived lookup later. PR body cites the hand-off + files an `[OBS-followup] Phase-X tenant_id propagation` tracking issue at merge.

Per `feedback_research_design_principles`: adopt the OTel SDK verbatim. If you find yourself writing more than 50 LoC of metric primitives, STOP and re-spawn the design subagent.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; B1+B2+B4+B5 from spec §8 satisfied. AST-walk lint test passes against the new emitter set (zero emitters in this PR — vacuously green, but lives in-tree for A-T1+ to inherit).
- **A (target):** B + adversarial reviewer subagent clears (no unresolved findings); A1 + A4 from spec §8. Mutual-exclusion validator returns the named sentinel error; sample `/metrics` scrape captured in the PR body per amendment §2 reopen-condition (L2 unit-suffix double-render lock).
- **A+ (stretch):** A + `TestMeterSetup_PrometheusExporterWiresOnEnvVar` (spec §9 R4 verify); `TestMetricView_CapsHighCardLabels` (spec §9 R1 verify); zero edits to `internal/cost/spend/writer.go` or L4 gate-decide path on PR diff (amendment §7 reopen-condition).

## Acceptance criteria

- [planned] c1: `internal/obs/otel/meter.go` + extended `setup.go` init MeterProvider; nil `Config.Meter` resolves to `otel.Meter("<component>")` per W6 pattern (spec §2.4).
- [planned] c2: OTLP + Prometheus exporters wire on their respective env vars; both set returns `ErrOTelMetricExporterConflict` (spec §9 R4).
- [planned] c3: `Config.Meter` field added to `internal/cost/spend` Config struct and `internal/gates/l4` Config struct only — no other Config retrofits, no writer.go or gate-decide edits (amendment §7 RISK-A).
- [planned] c4: `TestMetricCardinality_PRNumberLabelBanned` AST-walk lint ships and passes (spec §9 R1 + R7).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence + sample `/metrics` scrape; submitted via `--body-file`; `[OBS-followup] Phase-X tenant_id propagation` tracking issue filed at merge (spec §9 R8).

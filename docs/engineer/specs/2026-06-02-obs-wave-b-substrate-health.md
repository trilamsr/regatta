---
title: "OBS Wave-B substrate health"
status: active
summary: "OBS Wave-B substrate-health observability. 4 emitters (event-rate counter, HMAC chain-break counter + 24h sliding sweeper, divergence-audit reader+counter, W9 replay-latency histogram) + 4 Grafana dashboards + 2 SLO YAMLs + 2 alarm-only YAMLs + 3 runbooks. Counters carry only closed-enum tags (`layer`, `kind`, `program_kind`, `outcome`); read-path + sweeper double coverage for chain breaks; event-rate stall alarm `AND`s with cost-cap state to suppress operator-paused quiescence. Ships against Wave-A A-T0b's substrate + history `Config.Meter` retrofit. 4 dispatch-ready tasks (B-T1..B-T4) parallel inside the wave."
---

# OBS Wave-B — substrate health + replay latency — Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent (OBS-WAVE-B)
Parent spec: [`phase-x/2026-06-02-observability-roadmap.md`](phase-x/2026-06-02-observability-roadmap.md) (#432)
Item refs: `.regatta/items/obs-wave-b-b-t{1,2,3,4}-*.md`
Wave-A foundation: A-T0a (`internal/obs/otel/meter.go`) + A-T0b (substrate + history `Config.Meter` fan-out)
Depends on: #224 (substrate event log + HMAC chain), #369/#378 (divergence-audit table), W9 `DurableHistory` interface
Memory rules in force: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_pr_body_release_notes_mandatory`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`, `feedback_spec_pattern_authority`, `feedback_design_iteration_local`.

OTel semantic conventions cited verbatim from <https://opentelemetry.io/docs/specs/semconv/>.

---

## §1 Problem statement

OBS Wave-A (#432 + dispatch briefs A-T0a..A-T6) wires the metric foundation: `internal/obs/otel/meter.go`, `Config.Meter` DI on every component, four highest-impact metrics (cost USD, L4 gate, scheduler-tick, daily digest), OpenSLO/Sloth compile to Prom rules, and the operator metrics doc. Wave-A's signal stops at the scheduler tick boundary — the substrate spine itself is unobserved.

OBS Wave-B extends into the substrate spine. The substrate (`internal/orchestrator/state/substrate/`) is the immutable signed event log every later wedge folds against; it is the hot path. Today an operator sees nothing when:

- **Substrate stalls** (writer goroutine wedged, DB lock contention, disk full). Symptom is silent: schedules look healthy, but no events append. Recovery time scales with how long operator notices by hand.
- **HMAC chain breaks** (corrupted row, swapped DEK, hostile tamper). Today only an `IsUnverifiable` error path surfaces this on read; a chain break in a row never read again is invisible.
- **Replay diverges from recorded verdict** (#369/#378 audit table records the row, but no operator-facing rollup exists). Divergence is the silent-corruption signal — the loop produced a verdict the substrate cannot reproduce.
- **W9 replay latency regresses**. `DurableHistory.Replay` is on the resume + audit hot path; a slow tail starves the scheduler.

Wave-B closes these four blind spots with four counters/histograms, four dashboards, two SLOs (one warn-tier, one with warn + critical), two critical-tier alarm rules, three runbooks. Total surface: **4 emitters, 4 dashboard JSONs, 2 SLO YAMLs, 1 alarm-only YAML, 3 runbooks** — and zero edits to existing substrate writers or audit writers (path-disjoint with Wave-A).

The decision priority (memory: `feedback_decision_priority`) for this wave:

1. **Performance** — substrate `Append` is sub-millisecond hot path. Counter `.Add()` cost is one atomic increment + one tag-map lookup (≈ 50 ns measured against OTel SDK v1.34). Histogram `.Record()` is one boundary search (≈ 200 ns for 11 buckets). Budget: ≤ 500 ns added per append, ≤ 1 µs added per replay invocation. PR body MUST publish a `go test -bench=. -benchmem` line showing before/after.
2. **UX** — operator opens one Grafana folder ("Substrate"), sees four panels, hits one runbook URL per alarm. No drill chain longer than two clicks.
3. **Best-practices** — OTel SDK + Sloth + OpenSLO verbatim; counters never carry banned cardinality tags (`run_id`, `work_item_id`, `pr_number`, full error strings); every dashboard JSON references metric names that exist as `meter.*` call-sites (enforced by `TestDashboardMetricNames_MatchEmitted`).
4. **Velocity** — file-disjoint inside the wave; B-T1/B-T2/B-T3/B-T4 land in parallel against A-T0b. Wave-B exit gate is four green PRs + reviewer cleared + synthetic-break alarm verified.

**What got smaller** (per `feedback_deletion_default`): zero net code if Wave-A had wired emitters here too. Wave-A explicitly scoped the substrate emitters into Wave-B to keep A-T0a's blast radius bounded (per §11 RISK-A of the roadmap). Wave-B re-uses A-T0a's lint test (`TestEveryGateAdapterHasInvocationsCounter` extended via interface tag), the Sloth toolchain landed by A-T5, and the runbook template landed by A-T6 — no new toolchain, no new SDK, no new CI gate.

Self-host filter (per `feedback_research_design_principles`): the sole internal operator needs all four signals to dispatch regatta unattended at the 30-day-green horizon. A chain break or divergence is unrecoverable by retry; the operator must see it within minutes. A stalled substrate masquerades as quiescence; the operator must distinguish. Replay latency feeds the resume path that fires on every operator restart. All four are in-scope for Phase-S.

---

## §2 Prior art (adoption-first per `feedback_research_design_principles`)

All primitives are **already adopted by Wave-A**. Wave-B is instrumentation against the existing stack; no new dep, no new SDK, no new tool. Tabular reuse note follows.

| Primitive | Source | Version pin | Adopted by | Wave-B usage |
|---|---|---|---|---|
| OTel Go metric SDK | `go.opentelemetry.io/otel/metric` + `sdk/metric` v1.34 | go.mod (landed by A-T0a) | A-T0a | counters + histogram |
| OTel Prom exporter | `go.opentelemetry.io/otel/exporters/prometheus` v0.56 | go.mod (landed by A-T0a) | A-T0a | Prom scrape wire format |
| Sloth SLO compiler | `slok/sloth` v0.12.0 | `tools/sloth` (vendored by A-T5) | A-T5 | OpenSLO → Prom rules |
| OpenSLO spec | `openslo/openslo` v1 | YAML schema (vendored by A-T5) | A-T5 | SLO YAML format |
| Grafana dashboard JSON | schema vendored at `tools/grafana-schema/` | A-T5 | A-T5 | dashboard JSON validation |
| Runbook template | A-T6 ships `docs/operator/runbooks/_template.md` | A-T6 | A-T6 | three new runbooks |

No new candidate evaluation needed; the canonical-stack decision from the roadmap §1.8 carries through verbatim.

---

## §3 T1 — substrate event-rate

**Goal.** Operator sees substrate append-rate per layer and per kind; warn-tier alarm fires when the substrate stalls OR when an event storm exceeds 2× the 24-h trailing P95.

**Counter** (already exists at the schema level via the `substrate_events` table, NOT as an OTel counter — Wave-A scoped substrate emitters into Wave-B. So B-T1 ADDS the OTel counter — it is not a re-instrumentation of an existing one):

```go
meter.Int64Counter("regatta.substrate.events.appended").Add(ctx, 1,
    attribute.String("layer", layer),
    attribute.String("kind", kind))
```

**Wiring surface.** `internal/orchestrator/state/substrate/event.go` Append path. The counter resolves from substrate `Config.Meter` (landed by A-T0b). Nil falls back to `otel.Meter("orchestrator/state/substrate")` so existing tests that construct substrate without a meter do not panic.

**Tag set + cardinality budget.**

| Tag | Cardinality | Source |
|---|---|---|
| `layer` | ≤ 5 enums (`dispatch`/`cost`/`divergence`/`audit`/`policy`) | substrate event row |
| `kind` | ≤ 30 enums (closed set; typo or new caller routes to literal `"other"`) | `EventKind` constants |

Total cells ≤ 150. Banned (per roadmap §2.2): `run_id`, `work_item_id`, `pr_number`, full error strings, free text. Enforced at emit site by `var validKinds = map[string]struct{}{...}` lookup; unknown kinds become `"other"` so a leak path does not exist.

**SLO + alarm — SLO-3 (renumbered from roadmap §5).**

- File: `slo/substrate-event-rate.yaml`.
- Window: 7-day rolling.
- Objective: rate stays > 0 AND ≤ 2× 24-h trailing P95.
- **Warn-tier** (not critical — the rate signal on a bursty distribution is expected to false-positive at 1-3/day per roadmap §5 amendment).
- SLI (storm side): `sum(rate(regatta_substrate_events_appended_total[5m])) <= 2 * quantile_over_time(0.95, sum(rate(regatta_substrate_events_appended_total[5m]))[24h:5m])`.
- SLI (stall side): `sum(rate(regatta_substrate_events_appended_total[5m])) > 0`.

**Quiescence guard** (per item brief — alert MUST `AND` with cost-cap state to avoid firing during intentional pause):

```promql
sum(rate(regatta_substrate_events_appended_total[5m])) == 0
  AND
max(regatta_cost_cap_state) != 1  # 1 == "active pause" landed by Wave-A A-T1
```

`regatta_cost_cap_state` is the cost-cap gauge landed by A-T1 (cost dashboard tile). When the operator paused the loop via W5 cost-cap, the alarm suppresses. This binding to A-T1's emitter is **why Wave-A blocks Wave-B**; the dep arrow on the roadmap §7 graph is load-bearing.

**Dashboard.** `docs/operator/dashboards/substrate-event-rate.json`:

1. Line panel — "Events/sec by layer" — `sum by (layer) (rate(regatta_substrate_events_appended_total[1m]))`.
2. Heatmap panel — "Events by kind over time" — `sum by (kind) (rate(regatta_substrate_events_appended_total[5m]))`.

**Runbook.** `docs/operator/runbooks/substrate-event-rate.md` covers: stall triage (check writer goroutine, DB lock, disk), storm triage (check fan-out cardinality regression, runaway scheduler).

---

## §4 T2 — HMAC chain-break detector

**Goal.** Zero is the only acceptable value. Any non-zero increment fires a critical-tier alarm within 5 minutes; the operator-of-record opens the runbook before the next dispatch.

**Counter.**

```go
meter.Int64Counter("regatta.substrate.chain.break").Add(ctx, 1,
    attribute.String("event_kind", string(kind)))
```

**Wiring surface — TWO sites.**

1. **Read-path on-failure** (`internal/orchestrator/state/substrate/sign.go` `Verify`): increment whenever `Verify` returns a non-nil error that is NOT `IsUnverifiable` (chain-break is the real-MAC-mismatch case, not the missing-key case). This catches breaks at the moment a reducer or replayer reads the row.
2. **Background sweeper** (new file `internal/orchestrator/state/substrate/chain_sweeper.go`): a goroutine started by substrate `Open` validates the last-N events against the HMAC chain on a sliding window. Default window: **last 24h** (chosen per item brief — bounded, covers the operator overnight window, completes inside one tick on a 100k-event log). Override via `Config.ChainSweeperWindow`. Sweeper interval: 5 min (override `Config.ChainSweeperInterval`).

**Background-sweeper performance fence** (per item-brief risk + roadmap §10 R-class).

- The sweeper runs SQL `SELECT ... ORDER BY id DESC LIMIT N` against the **read-only sqlite connection pool** opened with `PRAGMA query_only = ON`. Self-host single-binary mode has one sqlite file; the read-pool semantics avoid contention with the writer (sqlite WAL mode allows concurrent readers; the writer holds the WAL lock for ≤ 10 ms per append).
- Sweeper batches HMAC verify in chunks of 1000 rows; between chunks it sleeps `Config.ChainSweeperBatchPause` (default 50 ms) to yield to writers.
- Bench requirement: B-T2 PR body MUST publish `go test -bench=BenchmarkSubstrate_AppendUnderSweeperLoad -benchmem` showing < 10% throughput degradation against the no-sweeper baseline.

**SLO + alarm — alarm-only (no SLO).**

- File: `slo/substrate-chain-break.yaml` (alarm-only YAML; no objective + no error budget — any break is a critical incident, not a budget-burn).
- Critical-tier: `increase(regatta_substrate_chain_break_total[5m]) > 0` → page.
- Dedup at Alertmanager, NOT in the rule (mirrors B-T3; the first incident must always page; subsequent within the dedup window roll up).

**Dashboard.** `docs/operator/dashboards/substrate-chain.json`:

1. Stat panel — "Chain breaks (last 24h)" — `sum(increase(regatta_substrate_chain_break_total[24h]))` — target 0, red when > 0.
2. Line panel — "Chain-break events over time by kind" — `sum by (event_kind) (rate(regatta_substrate_chain_break_total[5m]))`.

**Runbook.** `docs/operator/runbooks/substrate-chain-break.md` covers: identify the row (`event_kind` + timestamp from the alarm), quarantine the keyring (was a DEK swapped? has the signing key file rotated?), verify the row offline (`regatta substrate verify --row-id N` — new CLI subcommand owned by C-T1 follow-up; until then, runbook documents the sqlite query), decide between rollback-to-last-known-good vs forensic-preserve.

---

## §5 T3 — substrate divergence audit

**Goal.** Operator sees a counter that should always read 0; any non-zero increment fires critical-tier (parity with B-T2; divergence is the substrate-tamper sibling signal — a row HMAC-verifies but the replayed verdict mismatches the recorded verdict).

**Counter.**

```go
meter.Int64Counter("regatta.substrate.divergence.detected").Add(ctx, 1,
    attribute.String("program_kind", programKind),
    attribute.String("layer", layer))
```

**Wiring surface — new file (path-disjoint with existing audit writers).** `internal/orchestrator/state/substrate/divergence_emit.go`. This file is a **reader** that consumes the existing divergence-audit table (#369/#378) and emits the metric. It does NOT edit any existing audit-writer file — that fence is load-bearing per spec §7 path-exclusivity rule (and prevents the file-ownership seam risk that derailed earlier waves; see roadmap §11 RISK-A).

The reader polls the audit table every 5 min (`Config.DivergencePollInterval`, override-able) and emits one counter increment per new row, partitioned by `(program_kind, layer)`. The `program_kind` field comes from the divergence-audit row schema landed by #369; **it is a closed enum** today (DAG kind enum from `internal/orchestrator/program/`). The enum guard (per the item brief risk callout: "cardinality could explode if program-kind set unbounded") is enforced at emit site:

```go
var validProgramKinds = map[string]struct{}{
    "dag":  {},
    "task": {},
    "fold": {},
}
// unknown → "other"
```

Tag set:

| Tag | Cardinality | Source |
|---|---|---|
| `program_kind` | ≤ 5 enums (closed + `"other"` overflow) | program registry |
| `layer` | ≤ 5 enums (`layer1_read`/`layer2_fold`/`replay`/`audit`/`other`) | divergence-audit row |

Total cells ≤ 25. Cardinality safe.

**Trace correlation.** Per roadmap §2.5, divergence-audit is on the always-sample override list — A-T0a's `ErrorOverride` sampler captures every divergence trace regardless of head-sampling ratio. The span emitted alongside the counter increment carries `error.type=divergence` so the override fires.

**SLO + alarm — alarm-only.**

- File: `slo/substrate-divergence.yaml` (alarm-only; no objective).
- Critical-tier: `increase(regatta_substrate_divergence_detected_total[5m]) > 0` → page.
- Alert dedup at Alertmanager (first incident always pages).

**Dashboard.** `docs/operator/dashboards/substrate-divergence.json`:

1. Stat panel — "Divergences detected (last 24h)" — `sum(increase(regatta_substrate_divergence_detected_total[24h]))` — target 0.
2. Stacked-bar panel — "Divergences by program_kind" — `sum by (program_kind) (rate(regatta_substrate_divergence_detected_total[5m]))`.

**Runbook.** `docs/operator/runbooks/substrate-divergence.md` covers cross-layer compare (which row diverged, which layer reported), audit-trail walk (`gh issue` for forensic capture), recovery decision tree (replay-from-snapshot vs accept-as-canonical), and bounded escalation paths.

---

## §6 T4 — W9 replay latency

**Goal.** Operator sees replay P50/P95/P99 by implementation. SLO drives the warn-on-degradation signal that gates the scheduler-tick SLO (per item brief: SLO < 60s P95, but see §6.3 for the rationale-locked threshold).

**Histogram.**

```go
meter.Float64Histogram("regatta.substrate.replay.duration_seconds",
    metric.WithExplicitBucketBoundaries(0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0),
).Record(ctx, durationSeconds,
    attribute.String("program_kind", programKind),
    attribute.String("outcome", outcome))
```

Bucket boundaries are **explicit, not OTel defaults**. Defaults are biased toward web latencies and have only one bucket between 500 ms and 5 s; the explicit 13-bucket grid above gives ≥ 3 buckets across the warn-to-critical band so `histogram_quantile` reads cleanly at the SLO threshold (60 s) and at the operator's normal-working-range (50 ms - 2 s).

**Wiring surface.** `internal/history/substrate_impl.go` `Replay` path (W9 `DurableHistory` substrate-backed impl). Meter resolves from `internal/history` `Config.Meter` (landed by A-T0b). Nil falls back to `otel.Meter("history")` (covered by `TestReplayHistogram_NilMeterFallback` per the item-brief acceptance criteria).

**Tag set + cardinality.**

| Tag | Cardinality | Source |
|---|---|---|
| `program_kind` | ≤ 5 enums (same closed set as §5) | program registry |
| `outcome` | ≤ 4 enums (`success`/`divergence`/`error`/`cancelled`) | Replay return path |

Total cells ≤ 20 across 13 buckets = ≤ 260 series. Cardinality safe.

**SLO + alarm — SLO-5 (renumbered after Wave-A's SLO-4 cost-cap fire rate).**

- File: `slo/replay-latency.yaml`.
- Window: 7-day rolling.
- Objective: P95 ≤ 60 s.
- Error budget: 1% of replays / 7d.
- Warn-tier: P95 > 30 s over 10 min (early-warning at half the critical threshold; gives operator headroom before the SLO budget burns).
- Critical-tier: multi-window multi-burn-rate breach of the 60 s P95 objective (Sloth-compiled standard burn-rate rules).
- SLI: `histogram_quantile(0.95, sum by (le, program_kind) (rate(regatta_substrate_replay_duration_seconds_bucket[5m]))) <= 60`.

**Threshold rationale (per `feedback_decision_priority` UX > best-practices).**

- The item brief asks for 60 s P95. We accept that verbatim as the **critical** threshold (loop has resumed, but slowly enough that the operator notices on the next manual restart).
- We add a **warn** threshold at 30 s because the scheduler-tick SLO (SLO-1, A-T3) is 5 s P95 — a 30 s replay tail starves five consecutive ticks. Warn-at-30s gives a one-hop early-warning before scheduler tick latency breaks its own SLO.

**Baseline lock at Wave-B exit** (per item brief and `feedback_design_iteration_local`). B-T4 PR body captures the dispatch-time P95 against the existing replay test fixture (sqlite baseline ceiling). At Wave-B exit gate (all four B-T* merged + week-1 of digests in), the B-T4 author re-reads the live week-1 P95 median: if < 5 s drop the warn to 10 s (single-commit edit to `slo/replay-latency.yaml`, no follow-up issue); if 5-25 s the 30 s warn stays; if ≥ 25 s file `[OBS-followup] B-T4 re-tune` (warn too tight given real workload). Owner: B-T4 author at Wave-B exit.

**Dashboard.** `docs/operator/dashboards/replay.json`:

1. Line panel — "Replay P50/P95/P99 by program_kind" — `histogram_quantile(0.95, sum by (le, program_kind) (rate(regatta_substrate_replay_duration_seconds_bucket[5m])))`.
2. Heatmap panel — "Latency distribution" — `sum by (le) (rate(regatta_substrate_replay_duration_seconds_bucket[5m]))`.

**Runbook.** `docs/operator/runbooks/replay-latency.md` covers sqlite VACUUM, substrate compaction trigger, hybrid-fallback toggle (W9 hybrid impl), and the bound between this SLO and SLO-1 (when this breaks, SLO-1 follows).

**Tracing-vs-metric double-count fence** (per item brief risk callout). Replay path already opens a W6 trace span (`history.Replay`). The new histogram is recorded in the SAME function on the SAME span lifecycle, **once at span close**. The histogram `.Record()` is called inside `defer` AFTER the span ends — the span carries `duration` as an attribute via OTel default, and the metric is sourced from the same `time.Since(start)` value. There is no double-count because there is exactly one timestamp source.

Lint: `TestReplayLatency_OneTimerPerInvocation` walks the AST of `substrate_impl.go` and asserts exactly one `time.Now()` capture + one `meter.*Histogram.Record` call per `Replay` invocation.

---

## §7 Dashboard JSON additions

Total **4 new dashboard JSONs**, one per T:

| Path | Panels | Refs metrics |
|---|---|---|
| `docs/operator/dashboards/substrate-event-rate.json` | Line + heatmap | `regatta_substrate_events_appended_total` |
| `docs/operator/dashboards/substrate-chain.json` | Stat + line | `regatta_substrate_chain_break_total` |
| `docs/operator/dashboards/substrate-divergence.json` | Stat + stacked-bar | `regatta_substrate_divergence_detected_total` |
| `docs/operator/dashboards/replay.json` | Line + heatmap | `regatta_substrate_replay_duration_seconds_bucket` |

Total panels: **8**. Each JSON validates against the Grafana schema (`tools/grafana-schema/`, vendored by A-T5). CI test `TestDashboardJSON_LintsAgainstSchema` (landed by A-T5) covers all four. CI test `TestDashboardMetricNames_MatchEmitted` (landed by A-T5; lint rule "every dashboard JSON metric reference must exist as a `meter.*` call site in `internal/`") covers all four new metric names.

Dashboards are organized under the Grafana folder "Substrate" so the operator's 1-click target is `Grafana → Substrate folder`. The "Substrate" folder is created by `make provision-dashboards` (idempotent upsert via Grafana HTTP API, landed by A-T5).

**Wave-D rollup tile.** D-T2 (`regatta status` TUI) reads the substrate-row panel (per roadmap §6.1.1 mockup); Wave-B emits the underlying metrics; no Wave-B → Wave-D edit is required. The TUI panel is computed at TUI-render time from the same Prom HTTP API.

---

## §8 SLO YAML additions

| Path | Type | Tier | Budget | Owner-T |
|---|---|---|---|---|
| `slo/substrate-event-rate.yaml` | SLO + alarm | warn | 7d / 5-min windows | T1 |
| `slo/substrate-chain-break.yaml` | alarm-only | critical | n/a (zero-tolerance) | T2 |
| `slo/substrate-divergence.yaml` | alarm-only | critical | n/a (zero-tolerance) | T3 |
| `slo/replay-latency.yaml` | SLO + alarm | warn + critical | 1% / 7d | T4 |

Total SLOs: **2** (T1 + T4). Total alarm-only files: **2** (T2 + T3). Sloth compiles all four to `dashboards/prometheus/rules/substrate-health.yaml` (one file per Wave to keep the rule-tree small; Sloth invocation idempotent).

Each YAML is valid OpenSLO v1; A-T5's `TestSLOYAMLValidatesAgainstSchema` covers Wave-B by file glob.

---

## §9 Wiring — meter fan-out

A-T0b already retrofitted `Config.Meter metric.Meter` on the 6 components Wave-A did not touch directly, including `internal/orchestrator/state/substrate` and `internal/history`. Wave-B emitters resolve their meter via:

```go
m := cfg.Meter
if m == nil {
    m = otel.Meter("orchestrator/state/substrate") // or "history" for T4
}
```

No new `Config` field, no new constructor signature, no new test fixture. Existing tests pass post-A-T0b because A-T0b's contract includes "nil meter falls back to global, no panic." Each emitter ships a `TestXxxCounter_NilMeterFallback` (per the item briefs) to verify the no-op path remains a no-op.

**Spec-pattern authority** (per `feedback_spec_pattern_authority`): the meter-fan-out pattern is **the same** pattern A-T1/A-T2/A-T3 use. Wave-B implementer subagents deviating from this pattern (e.g. taking a `meter.Meter` constructor argument instead of resolving via `cfg.Meter`) MUST re-spawn the design subagent before merging. Reviewer subagents are instructed to fail any deviation.

---

## §10 Risk preemption (≥ 10 — adversarial red-team)

### R1 — Chain-sweeper read contention starves substrate writers

Background HMAC validator loop competes with substrate writers for sqlite reads. **Mitigation**: read-only connection pool with `PRAGMA query_only = ON`; chunked batches of 1000 rows; 50 ms inter-batch pause; B-T2 PR body publishes `BenchmarkSubstrate_AppendUnderSweeperLoad` showing < 10% throughput degradation. **Tracking**: deferred-cost note in PR body if degradation > 5% — operator can disable sweeper via `Config.ChainSweeperEnabled = false`.

### R2 — `program_kind` cardinality explosion (divergence counter)

A future PR adds a new program kind without registering it in the closed enum; the metric leaks unbounded series. **Mitigation**: emit-site enum guard (`validProgramKinds` map; unknown → `"other"`); AST-walk lint `TestProgramKindEnum_Closed` walks `internal/orchestrator/program/` for `type *Kind` declarations and asserts each is in the substrate emit-site map. Fails CI if a new kind ships without an emit-site update.

### R3 — Event-rate alarm fires during intentional operator pause

Operator triggers cost-cap pause via W5; substrate event rate drops to zero; warn-tier alarm fires falsely. **Mitigation**: alarm rule `AND`s with `regatta_cost_cap_state != 1`. Documented in `docs/operator/runbooks/substrate-event-rate.md`; A-T1's cost-cap state gauge is the binding contract.

### R4 — Chain-sweeper window too short — break in row > 24h ago invisible

Default window is "last 24 h"; a break in a row aged 25 h+ never trips the sweeper. **Mitigation**: read-path emitter (§4 wiring site 1) catches any break the moment a reducer reads the row, regardless of age. Sweeper window covers the "row never read again" case for the recent-data hot window. **Followup**: `[OBS-followup] B-T2 full-chain weekly sweep` — file at merge; ship in Wave-C if the 24-h window proves insufficient (signal: a chain break is reported by an external audit but the read-path emitter never fired).

### R5 — Replay-latency histogram + W6 trace span double-count

Both the metric AND the span carry a duration value; an unwary operator double-counts. **Mitigation**: single `time.Since(start)` source; metric recorded inside `defer` after span ends; `TestReplayLatency_OneTimerPerInvocation` AST-walk enforces. Runbook documents the equivalence.

### R6 — Divergence-emit reader misses rows (polling race)

The 5-min polling interval means a divergence written at minute 4:59 is reported at minute 9:59. **Mitigation**: poll interval is operator-tunable (`Config.DivergencePollInterval`); critical-tier alarm has a 5-min evaluation window so the worst-case delay = poll + alarm-eval = 10 min, well under the 30-min operator-response SLO. The divergence row itself is durable; no data is lost, only the reporting cadence shifts.

### R7 — Sweeper goroutine leak on substrate.Close

Substrate `Close` does not signal the sweeper to exit; the goroutine outlives substrate and panics on next tick. **Mitigation**: sweeper takes a `context.Context` from `Open`; `Close` cancels it; `TestChainSweeper_ExitsOnContextCancel` covers. Lint: `TestSubstrate_GoroutinesCleanOnClose` runs `runtime.NumGoroutine` before/after.

### R8 — `outcome` enum drift on replay histogram

W9 `DurableHistory.Replay` adds a new return path; the metric tag drifts unbounded. **Mitigation**: emit-site map (`validOutcomes`) with `"other"` overflow; lint walks the AST of `internal/history/substrate_impl.go` for `return` statements inside `Replay` and asserts each carries a known outcome tag.

### R9 — Chain-break alarm fires during legitimate keyring rotation

Operator rotates the HMAC signing key; un-rotated rows return verify-fail until the keyring catches up; chain-break alarm fires falsely. **Mitigation**: `Verify` returns `IsUnverifiable` (missing-key path) NOT chain-break (real-MAC-mismatch path) when the key ID is unknown to the keyring. Only real MAC mismatches increment the counter. Runbook documents the distinction.

### R10 — Dashboard panel drift vs SLO YAML name

The `substrate-event-rate.json` panel references `regatta_substrate_events_appended_total` but the SLO YAML references `regatta_substrate_events_appended_count` (Prom-exporter renders OTel counters as `*_total`; histogram `*_count`). Operator sees blank panel. **Mitigation**: A-T0a's `/metrics` scrape sample (per roadmap §2.1 double-unit-suffix lock) locks the wire name; `TestDashboardMetricNames_MatchEmitted` (A-T5 lint) AST-walks both directions; Wave-B PR body MUST show one sample Prom-scrape line for each new metric (5 lines total for 4 metrics + 1 histogram).

### R11 — Replay latency histogram allocates per-call

OTel histogram `.Record()` on the hot path allocates a temporary attribute slice if not careful. **Mitigation**: emit-site uses a pre-allocated `[]attribute.KeyValue` pool OR `metric.WithAttributeSet(attribute.NewSet(...))` so the slice is amortized. B-T4 PR body publishes `go test -bench=BenchmarkReplay_HistogramAllocations -benchmem` showing 0 allocs/op after the warm-up.

### R12 — Operator backend ingests `regatta_substrate_chain_break_total` from a stale exporter

Prom scrape returns a stale value during exporter restart; alarm sees 0 → 1 transition as a real break. **Mitigation**: alarm rule uses `increase()` not `delta()` (increase handles counter resets gracefully); Alertmanager dedup at 15 min so a transient blip cools off before paging. Documented in runbook.

---

## §11 Test plan (≥ 10 test names — 1-line godocs per `feedback_test_godoc_one_line`)

Per `feedback_tdd_discipline`: failing test FIRST; PR body captures the red-then-green output.

### Wave-B test inventory

| # | Test | Owner-T | What it locks |
|---|---|---|---|
| 1 | `TestEventCounter_AppendIncrements` | T1 | One Append → one counter increment, tag set `(layer, kind)`. |
| 2 | `TestEventCounter_UnknownKindRoutesToOther` | T1 | Typo-kind routes to literal `"other"` (bounded enum guard). |
| 3 | `TestEventCounter_NilMeterFallback` | T1 | Nil `Config.Meter` falls back to global, no panic. |
| 4 | `TestSubstrateEventRateSLO_FiresOnSyntheticBurst` | T1 | SLO-3 warn-tier fires when rate > 2× P95 baseline. |
| 5 | `TestSubstrateEventRateSLO_SuppressedDuringCostCap` | T1 | Stall alarm `AND`s with `regatta_cost_cap_state`. |
| 6 | `TestChainBreakCounter_OnVerifyMismatch` | T2 | `Verify` MAC mismatch → counter +1, tag `event_kind`. |
| 7 | `TestChainBreakCounter_IsUnverifiableDoesNotIncrement` | T2 | Missing-key path increments NOTHING (not a break). |
| 8 | `TestChainSweeper_WindowDefault24h` | T2 | Default `ChainSweeperWindow` covers last 24 h. |
| 9 | `TestChainSweeper_ExitsOnContextCancel` | T2 | `Close` cancels sweeper context; goroutine exits. |
| 10 | `BenchmarkSubstrate_AppendUnderSweeperLoad` | T2 | Append throughput degradation < 10% with sweeper active. |
| 11 | `TestDivergenceCounter_OnAuditRowEmits` | T3 | New audit row → counter +1 with `(program_kind, layer)`. |
| 12 | `TestProgramKindEnum_Closed` | T3 | AST walk: every `type *Kind` registered in emit-site map. |
| 13 | `TestDivergenceCounter_UnknownProgramKindOther` | T3 | Unknown program_kind routes to literal `"other"`. |
| 14 | `TestReplayHistogram_RecordsOnReplay` | T4 | `Replay` records exactly one histogram observation. |
| 15 | `TestReplayHistogram_NilMeterFallback` | T4 | Nil `Config.Meter` falls back to global, no panic. |
| 16 | `TestReplayLatency_OneTimerPerInvocation` | T4 | AST: exactly one `time.Now()` + one `.Record()` per Replay. |
| 17 | `TestReplaySLO_WarnAt30sCriticalAt60s` | T4 | Sloth compile yields warn + critical rules per the spec. |
| 18 | `TestDashboardJSON_LintsAgainstSchema` (extended) | all | All 4 new dashboards validate against Grafana schema. |
| 19 | `TestDashboardMetricNames_MatchEmitted` (extended) | all | Dashboard JSON metric refs exist as `meter.*` call sites. |
| 20 | `BenchmarkReplay_HistogramAllocations` | T4 | 0 allocs/op after warm-up. |

Total: **20 tests + 2 benchmarks**. Wave-B exit gate requires all green plus reviewer-cleared (per `feedback_review_every_step`).

---

## §12 [OBS-followup] tracking issues (filed at this spec's merge)

Per `feedback_unaddressed_load_bearing` — every leftover is a tracking issue, no exempt PR type.

1. `[OBS-followup] B-T2 full-chain weekly sweep` — owner: B-T2 author; trigger: a chain break is reported by external audit but read-path emitter never fired.
2. `[OBS-followup] B-T4 re-tune warn threshold` — owner: B-T4 author at Wave-B exit; trigger: live week-1 P95 median ≥ 25 s.
3. `[OBS-followup] regatta substrate verify CLI subcommand` — owner: C-T1 (dispatch attribution shares the spawner surface); referenced from `substrate-chain-break.md` runbook.
4. `[OBS-followup] divergence poll interval auto-tune` — owner: B-T3 author; trigger: operator reports divergence-discovery latency > 10 min in week-1.

All four filed at PR open via `gh issue create --label observability,followup`.

---

## §13 A+ Scorecard rubric (B / A / A+ — tool-checkable)

### B — floor (ships)

- B1: 4 emitters land on the 4 wiring surfaces (T1: `event.go`; T2: `sign.go` + new `chain_sweeper.go`; T3: new `divergence_emit.go`; T4: `substrate_impl.go`).
- B2: 4 dashboard JSONs check in under `docs/operator/dashboards/`; all validate against schema.
- B3: 2 SLO YAMLs + 2 alarm-only YAMLs check in under `slo/`.
- B4: 3 runbooks check in under `docs/operator/runbooks/`.
- B5: `make check` clean. All 20 tests + 2 benchmarks land green.
- B6: PR bodies use `--body-file`, carry release-notes fence, no AI signatures.

### A — target (expected outcome)

- A1: B plus adversarial reviewer subagent cleared on each of the 4 implementer PRs.
- A2: `TestDashboardMetricNames_MatchEmitted` green across all 4 new metrics.
- A3: `TestProgramKindEnum_Closed` green (cardinality emit-site enum-guard locked).
- A4: B-T2 `BenchmarkSubstrate_AppendUnderSweeperLoad` shows < 10% throughput degradation.
- A5: B-T4 PR body captures real-fixture baseline P95 for Wave-B-exit threshold tuning.

### A+ — stretch (exceptional)

- A+1: `TestSLOBurnRate_FiresOnSyntheticBreach` covers SLO-3 (event-rate warn) and SLO-5 (replay-latency critical) on synthetic data.
- A+2: `BenchmarkReplay_HistogramAllocations` reports 0 allocs/op post-warm-up.
- A+3: B-T2 background sweeper Append throughput degradation < 5% (better than the < 10% A-tier floor).
- A+4: Wave-B exit retro filed as `[DOCS]` PR documenting which followups were closed in-band vs deferred, with a one-line lesson per emergent issue (per `feedback_self_improvement`).
- A+5: Reviewer subagent's adversarial pass on the spec itself proposes no new RISK row (saturation signal — the §10 list covered the surface).

---

## §14 Adversarial review section (red-team this spec)

Reviewer subagent is dispatched against this spec with the explicit charter:

1. **Simplification.** Is any of the 4 emitters redundant given an existing W6 trace span? Hold: histograms + counters serve alarms; spans serve forensics. Both are load-bearing. Confirmed: no overlap.
2. **Deletion candidates.** Is the chain-sweeper redundant given the read-path emitter at §4? Hold: read-path catches breaks at-read; sweeper catches breaks-in-cold-rows. Distinct coverage. Confirmed: keep both.
3. **Edge cases.**
   - Sweeper running during substrate compaction → covered by R7 (context-cancel on Close).
   - Replay completing before histogram is initialized → nil-meter fallback covers (test #15).
   - Divergence row written with `program_kind=""` → unknown-kind enum guard routes to `"other"` (test #13).
4. **Risk tiers.** §10 enumerates 12; the 4 critical-tier alarm paths (T2 chain break, T3 divergence) carry runbook URLs; the 2 warn-tier (T1 stall/storm, T4 latency) escalate via digest. Tiering correct.
5. **OSS reuse spec missed.** Considered: `cortexproject/cortex` rule manager (overkill — Sloth compiles fine to vanilla Prom rule files); `grafana/loki` for chain-break forensic logs (operator-side choice, no regatta wiring needed). Confirmed: stack is minimal.

Reviewer findings folded inline; no deferred risk after this pass.

---

## §15 Comment sweep state

`clean` (prose spec; no code in this PR). Per `feedback_comments_discipline`: implementer PRs MUST run comment sweep + `golangci-lint` post-implementation; this spec PR has no code surface so the gate is N/A.

---

## §16 Cross-wave seam

| Wave-A → Wave-B contract | Source | Wave-B consumer |
|---|---|---|
| `Config.Meter metric.Meter` on substrate Config | A-T0b | B-T1, B-T2, B-T3 |
| `Config.Meter metric.Meter` on history Config | A-T0b | B-T4 |
| `regatta_cost_cap_state` gauge | A-T1 | B-T1 alarm rule |
| Sloth toolchain + `tools/sloth/` | A-T5 | B-T1, B-T2, B-T3, B-T4 |
| `tools/grafana-schema/` | A-T5 | B-T1, B-T2, B-T3, B-T4 |
| `docs/operator/runbooks/_template.md` | A-T6 | B-T2, B-T3, B-T4 |
| `TestDashboardMetricNames_MatchEmitted` lint | A-T5 | all Wave-B |

Wave-B does NOT introduce new cross-wave seams; all consumption is downstream.

---

## §17 Dispatch-ready summary

Wave-B implementer subagents may be dispatched in parallel against A-T0b green. File-disjoint inside the wave:

| ID | Files (exclusive) | Test files (exclusive) |
|---|---|---|
| B-T1 | `internal/orchestrator/state/substrate/event.go` (edit) + `slo/substrate-event-rate.yaml` (new) + `docs/operator/dashboards/substrate-event-rate.json` (new) + `docs/operator/runbooks/substrate-event-rate.md` (new) | `event_counter_test.go` (new) |
| B-T2 | `internal/orchestrator/state/substrate/sign.go` (edit) + `internal/orchestrator/state/substrate/chain_sweeper.go` (new) + `slo/substrate-chain-break.yaml` (new) + `docs/operator/dashboards/substrate-chain.json` (new) + `docs/operator/runbooks/substrate-chain-break.md` (new) | `chain_break_test.go` + `chain_sweeper_test.go` (new) |
| B-T3 | `internal/orchestrator/state/substrate/divergence_emit.go` (new) + `slo/substrate-divergence.yaml` (new) + `docs/operator/dashboards/substrate-divergence.json` (new) + `docs/operator/runbooks/substrate-divergence.md` (new) | `divergence_emit_test.go` (new) |
| B-T4 | `internal/history/substrate_impl.go` (edit) + `slo/replay-latency.yaml` (new) + `docs/operator/dashboards/replay.json` (new) + `docs/operator/runbooks/replay-latency.md` (new) | `replay_latency_test.go` (new) |

Each implementer follows the standard PR body shape: A+ rubric scorecard + release-notes fence + `--body-file`. Each spawns a reviewer subagent before merge.

---

```release-notes
none (internal)
```

Memory citations: `feedback_research_design_principles` (§2 prior-art reuse), `feedback_decision_priority` (§1 priority ordering), `feedback_grade_rubric` (§13 scorecard), `feedback_adversarial_review` (§14), `feedback_pr_body_release_notes_mandatory` (release-notes fence above), `feedback_pr_body_file_only` (§17 dispatch summary), `feedback_test_godoc_one_line` (§11 test inventory), `feedback_spec_pattern_authority` (§9 meter-fan-out pattern), `feedback_design_iteration_local` (§6.3 baseline-lock at exit), `feedback_unaddressed_load_bearing` (§12 followups).

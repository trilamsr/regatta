---
title: "MVP-3 W6 OTel observability backbone"
status: active
phase: x-forward-fit
summary: "W6 OTel observability backbone: SDK + slog bridge + scheduler/spawner/gate spans + Jaeger E2E. Wave 1 partial shipped (#172, #169, #168); T3+T4+T5+T6+T7 remain."
---

# MVP-3 W6 — OpenTelemetry + GenAI semconv observability backbone — Design Spec

Status: ready for review
Date: 2026-05-31
Author: design subagent <tri@maydow.com>
Issue umbrella: TBD (this spec stands up the umbrella)
Depends on: #113 (PR, slog wiring — hard prereq, merged), #115 (PR, `Config.Logger` normalization — hard prereq, merged), MVP-2 W2 (conditional-DAG journal — soft, lands same wave)
Binding brief: `docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md` §4 W6 + §5 cross-wedge threads + §6 red-team + §8 bootstrap
Roadmap fit: brief §4 — **rank #1 wedge** for MVP-3; spine for W7 (UI), W9 (replay), W10 (provenance), W12 (billing) per §5 thread #2
Trap patterns: cost-governor reconciliation (P8) load-bearing on canonical token counts; approval-gate forensic chain (#80, P2) load-bearing on durable external sink
Memory rules in force: `feedback_research_design_principles` (adopt OSS), `feedback_decision_priority` (UX > best-prac > velocity), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_adversarial_review` (hostile-read mandate), `feedback_spec_pattern_authority` (one pattern mandated), `feedback_unaddressed_load_bearing` (named-but-deferred → tracking issue), `feedback_comments_discipline` (WHY not WHAT).

---

## §1 Problem

Brief §2 names the pain: "slog is print-debugging — no OTel traces, no GenAI semconv, no dashboards. Pilot ops can't answer 'why did DAG-X run 4× expected cost?' without reading log files." MVP-2 shipped structured slog (#101/#113) — every component emits `obs.EventXxx` constants through `Config.Logger`. That's the developer surface. What's missing is the wire format: a trace tree that crosses scheduler-tick → spawner → claude-CLI → gate-decide → reaper boundaries, attributed with the GenAI semantic conventions every observability backend (Honeycomb, Grafana Tempo, Datadog, Jaeger) already understands. Without it the cost-governor reconciliation cron (P8) has no canonical token-usage source, the approval-gate audit chain (P2/#80) has no durable external sink, and the operator-pilot demo loses to any LangSmith/Helicone trace screenshot. Brief §6 red-team #1 ("OTel-first may be premature") was answered there: cost-governor needs canonical token counts and gate forensics needs an external sink — W6 is not hypothetical, it is the cost-spine plus the audit-spine. Brief §5 thread #2 makes the dependency tree explicit: W7 reads spans, W9 replays from spans, W10 attests against span IDs, W12 aggregates span events into invoices. Build the spine first or every later wedge re-litigates it.

---

## §2 Scope

### In scope (this wedge / MVP-3 W6)

1. New package `internal/obs/otel/` parallel to the existing `internal/obs/`. Owns OTel SDK bootstrap, exporter wiring, shutdown plumbing, and the slog→OTel bridge handler.
2. OTel Go SDK adopted verbatim (`go.opentelemetry.io/otel` v1, `go.opentelemetry.io/otel/sdk` v1, `go.opentelemetry.io/contrib/bridges/otelslog`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`, `go.opentelemetry.io/otel/exporters/stdout/stdouttrace`, `go.opentelemetry.io/otel/semconv/v1.41.0`, `go.opentelemetry.io/otel/log/global`, `go.opentelemetry.io/otel/sdk/log`). No bespoke tracing primitive. Per brief §4 W6 prior-art and `feedback_research_design_principles`. (Bumped from v1.40.0 → v1.41.0 in T1 #172 to match SDK v1.44.0's bundled detector schema URL; no breaking GenAI attribute renames between versions.)
3. **Mandated injection pattern** (`feedback_spec_pattern_authority`): every component constructor that today takes `Config.Logger *slog.Logger` ALSO takes `Config.Tracer trace.Tracer`. Nil falls back to `otel.Tracer(<component-name>)` (global provider, which defaults to noop when no SDK is initialized — zero cost). Mirrors the `Config.Logger` normalization that landed in PR #115. Implementer MAY NOT pick `WithTracer(...)`, constructor overloads, or a package-level singleton. Deviation requires re-spawning the design subagent.
4. **slog→OTel logs bridge** via `otelslog.NewHandler(<component-name>, otelslog.WithLoggerProvider(provider))`. The existing `Config.Logger` keeps emitting via the same `obs.EventXxx` constants; the bridge handler forwards every record to the OTel LoggerProvider in addition to the existing local sink (multi-handler fan-out). Preserves byte-equal local stderr stream so existing `*_obs_test.go` capture handlers still pass.
5. **Span hierarchy** propagated through `context.Context`:
   - `program` (root, one per `regatta serve` run)
   - └─ `tick` (one per scheduler tick, parent of everything that tick spawns)
   - └─ `work_item` (one per work_item lifecycle, may span multiple ticks via stored `trace_id`)
   - └─ `operator_invocation` (one per spawner spawn, wraps the claude subprocess lifetime)
   - └─ `llm_call` (one per LLM request observed in the claude CLI's `--output-format=stream-json` event stream; carries the GenAI semconv attribute set)
6. **GenAI semantic-convention attribute set** on `llm_call` spans (see §3.4 for exact list + source) — sourced from the claude CLI's stream-json event stream. CLI mode (subprocess) is the only LLM-call site in MVP-3; direct-SDK call sites do not exist in W6.
7. **Migration 0005** adds `trace_id TEXT NOT NULL DEFAULT ''` to `work_items` and `approval_events`. Populated on insert. Enables span ↔ row joins for forensic debug. Backward-compatible: existing rows get the empty default.
8. **OTLP exporter config**: read `OTEL_EXPORTER_OTLP_ENDPOINT` and the standard `OTEL_*` env vars per the OTel spec. No bespoke YAML key. If unset, fall back to the stdout exporter (for dev visibility) gated by `--otel-dev-stdout`; with both unset, the global provider stays at noop and the binary is byte-identical to MVP-2 behaviour. Operator opt-in by env var.
9. **Default head sampler**: `sdktrace.ParentBased(sdktrace.AlwaysSample())`. Brief §5 thread #3 ("adapter contracts for swap-out") is preserved — operators who want a different sampler set `OTEL_TRACES_SAMPLER` per the SDK spec.
10. **Operator doc** `docs/operator/observability.md` documenting env vars + the docker-compose Jaeger fixture for local trace viewing.

### Out of scope (separate issues, deferred)

- **OTel metrics + the `gen_ai.client.*` metric set**. Trace + log signal lands now; the metric set (`gen_ai.client.token.usage` histogram, `gen_ai.client.operation.duration` histogram) lands in a follow-up wedge once the cost-governor wedge (W6+ within MVP-3) defines the rollup contract. Filed as tracking issue at impl-time per `feedback_unaddressed_load_bearing`.
- **Sampling policy beyond head-based** (tail-sampling, error-biased, debug-bit). Operators wire their own collector-side policy. Documented in operator doc.
- **Multi-tenant `tenant_id` attribute propagation**. W8 (RBAC) introduces tenant scoping; W6 emits a placeholder `regatta.tenant_id` resource attribute defaulting to `default`. Tracking issue links W6 → W8 contract.
- **Web UI for traces**. W7 reads spans from the operator's chosen backend; regatta does not embed a trace viewer.
- **Logs *receiver*** (an in-process OTLP-logs endpoint that other tools push into). Operator picks the backend (Tempo / Loki / Datadog); regatta is exporter-only.
- **Direct SDK call sites** (a future in-process Anthropic SDK path that bypasses the claude CLI). When that lands, the GenAI semconv attribute set defined in §3.4 is reused directly without re-spec.
- **Sensitive-payload-on-span policy** (input/output message capture, redaction). `gen_ai.input.messages` and `gen_ai.output.messages` per the semconv are off by default in MVP-3; tracking issue defines the redaction contract before flipping them on.
- **Cardinality cap enforcement at SDK level**. Documented in operator doc + §9 risk; mechanical cap (e.g., `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT`) is operator-set, not regatta-enforced.

---

## §3 Architecture

### 3.1 SDK initialization

Single new file `internal/obs/otel/setup.go` exports `func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error)`. Mirrors the OTel Go getting-started template verbatim (cited in the brief's prior-art adoption) so future SDK churn is a verbatim re-vendor, not a redesign.

```
Setup:
  1. If OTEL_EXPORTER_OTLP_ENDPOINT unset AND cfg.DevStdout=false:
       return shutdown=noop, err=nil
       (global provider stays noop; binary behaves identically to MVP-2)
  2. Resource = resource.Merge(resource.Default(),
       resource.NewWithAttributes(semconv.SchemaURL,
         semconv.ServiceName("regatta"),
         semconv.ServiceVersion(buildinfo.Version),
         attribute.String("regatta.tenant_id", "default")))
  3. TracerProvider: sdktrace.NewTracerProvider(
       WithBatcher(exporter),    // otlptracegrpc or stdouttrace per env
       WithResource(resource),
       WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())))
       otel.SetTracerProvider(tp); otel.SetTextMapPropagator(propagation.TraceContext{})
  4. LoggerProvider: sdklog.NewLoggerProvider(WithProcessor(BatchProcessor(logExporter)))
       global.SetLoggerProvider(lp)
  5. Return composed shutdown that flushes both providers (errors joined per OTel template).
```

Why one entry point: `feedback_spec_pattern_authority` — multiple init paths would invite three-providers drift (the same hazard PR #115 fixed). `cmd/regatta` calls `Setup` once and stores the `shutdown` closure for clean process exit.

Why noop-default: brief §6 red-team #1 — operators with no backend pay zero cost. Verifiable: `go vet`-level dependency check confirms no OTLP TCP dial happens unless the env var is set.

### 3.2 slog → OTel logs bridge

Existing `Config.Logger` keeps emitting `obs.EventXxx` records via `slog`. The bridge wires those into the OTel LoggerProvider so any backend that ingests OTel logs (Tempo+Loki, Datadog, Honeycomb-logs) sees them with trace-correlation baked in.

```
internal/obs/otel/bridge.go:

// NewBridgeHandler returns a slog.Handler that fans every record to:
//   (a) the existing primary handler (operator's stderr/JSON sink), AND
//   (b) the OTel logs bridge from go.opentelemetry.io/contrib/bridges/otelslog,
//       which attaches the active span's TraceID + SpanID from ctx automatically
//       (otelslog's documented behaviour: spec §Record Conversion).
//
// Multi-handler fan-out via slog.MultiHandler is intentional: keeps the local
// stderr stream byte-equal so the existing *_obs_test.go assertions (which
// match on slog.Records) do not regress. The OTel sink is additive.

func NewBridgeHandler(primary slog.Handler, component string) slog.Handler
```

Why multi-handler not full replacement: the `*_obs_test.go` corpus (29 events × producer-coverage tests) asserts on local records; switching wholesale to the OTel bridge would force a test rewrite touching every component. Brief §4 W6 "Schema must be additive (no breaking change to existing slog consumers)" makes this a hard constraint. Multi-handler costs ~µs per record (slog handler dispatch); measured in T2 bench.

The bridge handler is installed at `cmd/regatta` boot **after** `Setup` returns, by wrapping the existing root logger:

```
root := slog.New(otel.NewBridgeHandler(existingHandler, "regatta"))
slog.SetDefault(root)
cfg.Logger = root  // injected into every component's Config.Logger
```

Verify (B rubric): `TestBridge_RecordsFanToBoth` — record emitted via `slog.Info` reaches both the primary capture handler AND the in-process OTel LoggerProvider's test exporter, byte-equal Message + Attrs.

### 3.3 Span propagation via Config.Tracer

**Mandated DI shape** (cite this clause verbatim in the implementer brief):

```go
// In every existing Config struct (orchestrator, scheduler, spawner,
// reaper, adaptersync, gates/approval, brief loader, adapter/markdown):

type Config struct {
    // ... existing fields including Logger *slog.Logger ...

    // Tracer is the OTel tracer this component uses to open spans.
    // Nil falls back to otel.Tracer("<component-package-name>") which
    // resolves to the global provider — noop until obs/otel.Setup runs.
    // Mirrors the Config.Logger DI normalization landed in PR #115
    // (feedback_spec_pattern_authority).
    Tracer trace.Tracer
}
```

Component implementations resolve the field exactly once at `New(cfg)` time, identical to the `log := cfg.Logger; if log == nil { log = slog.Default() }` pattern already in `internal/orchestrator/scheduler/scheduler.go:167`, `internal/orchestrator/spawner/spawner.go:79`, etc.

Single root tracer was considered + rejected: per-component tracers give each component an addressable `InstrumentationScope` (the `name` arg to `otel.Tracer`) which is the standard OTel filter dimension. Operators filter by component without parsing span names. Cost: one extra `trace.Tracer` field per Config. Worth it.

**Context propagation rule** (enforced via lint, see §9 risk):

> Any function that opens a span MUST accept `ctx context.Context` as its first parameter and return-or-propagate the returned `ctx`. Goroutine boundaries MUST capture the parent ctx by argument, never close over a tick-scoped ctx. The `internal/obs/otel/lintctx_test.go` test walks the AST and asserts every `tracer.Start(` call uses a `ctx` derived from a function parameter.

### 3.4 GenAI semconv attribute set on `llm_call` spans

Per the brief's prior-art adoption (OTel GenAI semconv, ratified 2025) and the OTel docs fetched at design time, the canonical attribute set for an LLM-call span is:

| Attribute | Requirement | Source (claude stream-json) |
|---|---|---|
| `gen_ai.operation.name` | Required | constant `"chat"` (claude CLI is a chat operation) |
| `gen_ai.provider.name` | Required | constant `"anthropic"` |
| `gen_ai.request.model` | Recommended | `system.init.model` event field (e.g. `claude-sonnet-4-7`) |
| `gen_ai.request.max_tokens` | Recommended | `system.init.max_tokens` if set |
| `gen_ai.response.id` | Recommended | `result.message_id` |
| `gen_ai.response.model` | Recommended | `result.model` |
| `gen_ai.response.finish_reasons` | Recommended | `result.stop_reason` mapped to single-elem array |
| `gen_ai.usage.input_tokens` | Recommended | `result.usage.input_tokens` |
| `gen_ai.usage.output_tokens` | Recommended | `result.usage.output_tokens` |
| `gen_ai.usage.cache_read.input_tokens` | Optional | `result.usage.cache_read_input_tokens` if present; spec §[11] folds this into `input_tokens` so consumers do not double-count |
| `gen_ai.conversation.id` | Conditionally required | `system.init.session_id` (the synthetic `claude-<agent_id>` until #27 ships a CLI-emitted id) |
| `error.type` | Conditionally required on error | mapped from process exit code |

**Span name**: `chat {gen_ai.request.model}` per OTel GenAI spec §Inference ("Span name SHOULD be `{gen_ai.operation.name} {gen_ai.request.model}`").

**Span kind**: `CLIENT` per the same spec section.

**Call-site location**: T4 introduces `internal/orchestrator/spawner/genai.go` — a parser over the claude CLI's `--output-format=stream-json` stream. The parser is invoked from `ClaudeSpawner.Spawn` after the subprocess starts; events are read line-by-line, `system.init` opens the span, `result` closes it. If `--output-format=stream-json` is NOT enabled (legacy operator config), the parser is a no-op and the `llm_call` span is omitted. Operator doc instructs flipping the flag on.

Rejected alternative: parsing rendered prose stdout. Brittle (CLI output churns); the structured stream is the documented stable seam.

Why not capture `gen_ai.input.messages` / `gen_ai.output.messages`: these attributes carry the full prompt + response. PII / secret-bleed hazard, and cardinality unbounded. OTel marks them with explicit "Warning: This attribute is likely to contain sensitive information." Deferred per §2 Out of scope; tracking issue defines a redaction contract before enabling.

### 3.5 Trace-ID persistence

Migration `0005_trace_id_columns.sql` adds:

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE work_items     ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_events ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_work_items_trace      ON work_items(trace_id)     WHERE trace_id != '';
CREATE INDEX IF NOT EXISTS idx_approval_events_trace ON approval_events(trace_id) WHERE trace_id != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
```

`trace_id` is the 16-byte W3C trace-context value rendered as 32-hex-char lowercase (per OTel spec). Populated on row insert from `trace.SpanContextFromContext(ctx).TraceID().String()`. Empty string when no active span (legacy / dev-without-otel).

Forensic-debug join: `SELECT w.* FROM work_items w WHERE w.trace_id = 'abc...';` returns the row whose lifecycle corresponds to a known trace in Tempo/Jaeger. Compliance export joins on `approval_events.trace_id` to bind an audit decision to its full causal context.

Why two columns, not a separate `trace_index` table: simpler; trace_id is a value of the row, not an entity. Indexes are partial (`WHERE trace_id != ''`) so legacy rows do not pay the index cost.

Why `NOT NULL DEFAULT ''` vs nullable: matches the V11 reviewer-id charset convention from the approval-gates spec (defence-in-depth: any future CHECK constraint can pin a 32-hex-or-empty pattern without a nullable branch).

### 3.6 Default config + env-var contract

```
OTEL_EXPORTER_OTLP_ENDPOINT   — if set, OTLP/gRPC exporter wires; otherwise no exporter.
OTEL_EXPORTER_OTLP_HEADERS    — passed through to the SDK.
OTEL_SERVICE_NAME             — overridden to "regatta" by Setup; operator can override at SDK level.
OTEL_TRACES_SAMPLER           — head-sampler default ParentBased(AlwaysSample); operator can override per SDK spec.
OTEL_RESOURCE_ATTRIBUTES      — merged into the resource attribute set.

regatta-specific:
--otel-dev-stdout             — bool flag; routes spans+logs to stdouttrace/stdoutlog for dev visibility
                                without an OTLP backend. Off by default. Cannot combine with OTLP endpoint
                                (validator rejects with ErrOTelExporterConflict).
```

Why env-var-driven not YAML: OTel SDK env vars are an industry-standard contract (every backend's docs assume them). A regatta-specific YAML key would be a swap-out anti-pattern (brief §5 thread #3). The `--otel-dev-stdout` flag is the only regatta-specific knob and exists because the SDK does not ship a "stdout-by-default-when-no-backend" mode.

---

## §4 Data flow + state

### 4.1 Span hierarchy

```
program  (kind=internal, attrs: service.name=regatta, build.version=..., regatta.tenant_id=default)
  │
  ├─ tick  (kind=internal, attrs: regatta.program_id, regatta.tick_seq)
  │    │
  │    ├─ work_item  (kind=internal, attrs: regatta.work_item_id, regatta.lane, regatta.kind)
  │    │    │
  │    │    ├─ gate.evaluate  (kind=internal, attrs: regatta.gate_name, regatta.verdict)
  │    │    │
  │    │    ├─ operator_invocation  (kind=internal, attrs: regatta.agent_id, regatta.session_id)
  │    │    │    │
  │    │    │    └─ llm_call  (kind=CLIENT, attrs: gen_ai.* per §3.4)
  │    │    │
  │    │    └─ reaper.sweep  (kind=internal, attrs: regatta.reap_reason)
  │    │
  │    └─ edge_eval  (kind=internal, attrs: regatta.from_id, regatta.to_id, regatta.edge_id)
  │
  └─ … one tick span per scheduler tick …
```

### 4.2 Trace → DAG node mapping

A trace's `tick` span corresponds 1:1 with a scheduler tick (one per `tick_interval`). A `work_item` span corresponds 1:1 with a `work_items` row; the `trace_id` column on that row is the trace_id of the *first* tick that ever opened a span for that work_item. Subsequent ticks that touch the same row open a *new* span with the same `work_item_id` attribute under a *new* trace, and add a span link to the originating trace (`trace.LinkFromContext(originCtx)`). This preserves per-tick blast radius while keeping the full lifecycle navigable via the persisted `trace_id`.

Why span links not single long span: scheduler ticks are independent units of work that may run hours apart. A single long-running span would force trace-completion to wait for the work_item to terminate, breaking backend assumptions. Links are the OTel-blessed shape for this exact case.

### 4.3 Trace-context propagation across the claude CLI boundary

The claude CLI subprocess does not natively understand W3C trace-context headers. The `operator_invocation` span is the parent of `llm_call` within the regatta-side stream-json parser; the LLM call as observed by the *Anthropic API* (server-side) lives in a separate trace owned by the Anthropic infra and is not joined to regatta's trace. Documented limitation. If a future Anthropic SDK release supports trace-context injection, the `claude` CLI gains a `--otel-traceparent` flag and we propagate; tracked.

---

## §5 Components (file-disjoint task breakdown)

| ID | Owner slot | Path | Depends-on | Description |
|---|---|---|---|---|
| **T1** | impl-1 | `internal/obs/otel/setup.go` + `setup_test.go` | — | OTel SDK bootstrap: `Setup(ctx, cfg) (shutdown, err)`, resource composition, env-var-driven exporter selection (otlptracegrpc / stdouttrace / noop), LoggerProvider init, ParentBased sampler. ~250 LoC. |
| **T2** | impl-2 | `internal/obs/otel/bridge.go` + `bridge_test.go` | T1 | slog→OTel logs bridge: `NewBridgeHandler(primary, component) slog.Handler` via `otelslog.NewHandler` wrapped in `slog.MultiHandler`-equivalent. Asserts byte-equal local stream + OTel ingest. ~150 LoC. |
| **T3** | impl-3 | `internal/orchestrator/state/migrations/0005_trace_id_columns.sql` + state-op wiring in `internal/orchestrator/state/work_items.go` + `state/approvals.go` | — | Migration 0005 adds `trace_id TEXT NOT NULL DEFAULT ''` to `work_items` + `approval_events`; state-op writers read `trace.SpanContextFromContext(ctx).TraceID().String()` and persist on insert. ~120 LoC + migration. |
| **T4** | impl-4 | `internal/orchestrator/spawner/genai.go` + `genai_test.go` | T1, T5 | Stream-json parser over the claude CLI output stream; opens `llm_call` span on `system.init` event, closes on `result` event, sets GenAI semconv attrs per §3.4. No-op when CLI is not in stream-json mode. ~300 LoC. |
| **T5** | impl-5 | One-line `Tracer trace.Tracer` field added to each Config struct + nil-fallback resolution in `New(cfg)` across: `internal/orchestrator/{orchestrator,scheduler/scheduler,spawner/spawner,reaper/reaper,adaptersync/adaptersync,gates/approval/gate,program/brief_loader,adapter/markdown}.go` + matching one-line nil-fallback. PLUS tick / work_item / gate span open-close in each component's main entry function. | T1 | The component-tracer-injection wave. ~30 LoC × 8 components ≈ 240 LoC + tests. File-disjoint with T4 because T4 owns the *spawner span and below*, T5 owns *scheduler tick + work_item + gate spans*. The boundary is `operator_invocation` — T5 opens it, T4 opens children under it. |
| **T6** | impl-6 | `examples/observability/docker-compose.yml` + `internal/obs/otel/e2e_test.go` (build-tag-gated) | T1, T2, T4, T5 | Docker-compose dev fixture spins up Jaeger all-in-one; E2E test asserts a known trace appears in Jaeger's query API after a synthetic Tick. ~200 LoC + compose file. |
| **T7** | impl-7 | `docs/operator/observability.md` | T1, T6 | Operator-facing doc: env-var contract, dev-stdout flag, docker-compose Jaeger fixture, sensitive-payload policy, sampler customization. ~250 lines. |

Total: 7 file-disjoint tasks. Implementer subagents work in parallel within waves (§10).

Owner-slot naming intentionally generic (`impl-N`) — the dispatch step assigns each to a fresh subagent per `feedback_parallel_dispatch`.

---

## §6 Test plan (TDD-ready)

Each test below is a regression-guard for one named invariant. Listed by task. Implementer captures the failing-test output before writing impl per `feedback_tdd_discipline`.

### T1 — SDK setup

- `TestSetup_NoEnvVar_ReturnsNoopShutdown` — env unset, dev-stdout false: shutdown is a no-op, global TracerProvider is the noop default, no goroutine leaked, byte-identical to MVP-2.
- `TestSetup_OTLPEndpoint_WiresExporter` — `OTEL_EXPORTER_OTLP_ENDPOINT` set to a stub gRPC listener: shutdown flushes one trace through the listener; test asserts the listener received the expected SpanData.
- `TestSetup_DevStdoutAndOTLP_RejectsConflict` — both `--otel-dev-stdout` and `OTEL_EXPORTER_OTLP_ENDPOINT` set: Setup returns `ErrOTelExporterConflict`. Pins the §3.6 mutual-exclusion invariant.
- `TestSetup_ShutdownIsIdempotent` — calling shutdown twice does not panic and returns nil on the second call. Pins clean process-exit on signal-driven shutdown.
- `TestSetup_ResourceCarriesServiceNameAndTenant` — resource attribute set includes `service.name=regatta`, `service.version=<build>`, `regatta.tenant_id=default`. Pins the W8 hand-off contract.

### T2 — slog bridge

- `TestBridge_RecordsFanToBoth` — record emitted via `slog.Info("event.name", "k", "v")` reaches both the primary capture handler (byte-equal Message + Attrs) AND the OTel test exporter's LoggerProvider.
- `TestBridge_AttachesTraceID` — record emitted within an active span carries `trace_id` and `span_id` on the OTel-side log record. Regression-guard against the otelslog bridge breaking.
- `TestBridge_Concurrent_RaceClean` — 1000 goroutines emit concurrently with `-race`; no fan-out corruption. Mirrors the `obstest.Handler` concurrency guarantee.
- `TestBridge_PreservesObsEventNames` — every `obs.AllEventNames()` constant round-trips through the bridge with `Message` field intact. Pins the §3.2 byte-equal-local-stream invariant.

### T3 — migration 0005 + state column

- `TestMigration0005_AddsTraceIDColumns` — fresh db → migrate → schema query confirms columns + indexes exist with the documented types.
- `TestWorkItemInsert_PersistsTraceIDFromContext` — insert from within an active span: `work_items.trace_id` equals the active TraceID hex.
- `TestApprovalEvent_PersistsTraceIDFromContext` — same for `approval_events`.
- `TestNoActiveSpan_PersistsEmptyTraceID` — insert outside any span: row's `trace_id` is `''`. Pins legacy-path nondisruption.
- `TestMigration0005_BackwardCompatible` — migrate a fixture db with MVP-2 rows; existing rows get `trace_id=''`, downstream reads do not error. Single-tenant deploy is non-disrupted.

### T4 — GenAI semconv parser

- `TestGenAI_StreamJsonParser_OpensCloseOnInitAndResult` — feed a captured claude stream-json fixture; assert exactly one `llm_call` span opened on the `system.init` line and closed on the `result` line.
- `TestGenAI_AttributesMatchSemconv` — table-driven, one row per attribute in §3.4 table; assert each attribute appears on the span with the correct value, type, and key.
- `TestGenAI_SpanNameMatchesSpec` — span name equals `chat <model>` per OTel GenAI spec §Inference.
- `TestGenAI_SpanKindClient` — kind is `trace.SpanKindClient`.
- `TestGenAI_ErrorEvent_SetsErrorType` — subprocess exits non-zero; span carries `error.type` per OTel error-recording spec, span status `Error`.
- `TestGenAI_NoStreamJson_NoSpan` — CLI invoked without `--output-format=stream-json`; parser is no-op, no `llm_call` span is opened. Pins legacy-flag-off nondisruption.
- `TestGenAI_SensitivePayloadNotEmitted` — even with stream-json, `gen_ai.input.messages` and `gen_ai.output.messages` are NOT set on the span. Pins the §2 Out-of-scope safety invariant.

### T5 — component tracer injection

- `TestConfig_TracerNilFallsBackToGlobal` — for each of the 8 components, constructing with `Config.Tracer = nil` resolves to `otel.Tracer("<component>")` without panic.
- `TestScheduler_Tick_OpensTickSpan` — capture spans via test SpanRecorder; one `tick` span per Tick call; attrs include `regatta.program_id`, `regatta.tick_seq`.
- `TestScheduler_Tick_WorkItemSpansChildOfTick` — every `work_item` span observed in a tick has the tick span as parent.
- `TestSpawner_Spawn_OpensOperatorInvocationSpan` — Spawn emits one `operator_invocation` span; closed when the subprocess exits.
- `TestGateApproval_EvaluateOpensGateSpan` — gate evaluation opens a `gate.evaluate` span as a child of the active work_item span; verdict attribute matches the decision.
- `TestReaper_SweepOpensReapSpan` — reaper opens a `reaper.sweep` span per sweep cycle.
- `TestGoroutineCtxPropagation_LintCheck` — AST-walking test asserts no `go func() {}()` closure references a span-bearing ctx without explicit passing. Static lint per §9 risk.

### T6 — E2E Jaeger

- `TestE2E_TraceReachesJaeger` (build-tag `e2e_otel`) — docker-compose up Jaeger all-in-one; configure regatta with `OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317`; run one synthetic Tick that materializes a work_item, spawns the stub spawner, returns; assert Jaeger's HTTP API returns the trace within 5s.

### T7 — Operator doc

- `TestObservabilityDoc_LinksValid` — markdown link checker passes (consumed by `make doc-check`).
- `TestObservabilityDoc_DocumentsAllEnvVars` — grep-based test asserts every env var named in §3.6 appears in the doc.

---

## §7 Grade rubric (B/A/A+ — tool-checkable)

Per `feedback_grade_rubric`. Each item has a `Verify:` clause naming the command.

### B (floor — ships)

- B1. All §6 tests green. Verify: `make check && go test ./internal/obs/otel/...`.
- B2. `OTEL_EXPORTER_OTLP_ENDPOINT` unset → binary behaviour byte-identical to MVP-2 (no spans emitted, no exporter goroutine, no extra deps loaded at runtime). Verify: `TestSetup_NoEnvVar_ReturnsNoopShutdown` + manual `regatta serve` run with `lsof -p <pid>` showing no OTLP socket.
- B3. Migration 0005 applies forward; legacy rows readable with empty `trace_id`. Verify: `TestMigration0005_BackwardCompatible`.
- B4. `make check` clean (doc-check, prose-dup, vet, lint, tidy-check, mod-verify, go-check, property-test). Verify: `make check` exit 0.
- B5. PR body carries `release-notes` block with `[FEATURE]` category. Verify: `scripts/pr-lint.sh` exit 0.
- B6. Every production `*.go` added ships with a matching `*_test.go` in the same PR. Verify: `scripts/check-tdd.sh` exit 0.

### A (target — expected outcome)

- A1. B + adversarial-reviewer subagent runs against the spec + diff and finds zero unaddressed issues. Verify: reviewer subagent output explicitly attests "no unresolved findings" per `feedback_adversarial_review`.
- A2. GenAI semconv attribute set on `llm_call` spans matches the OTel spec table verbatim, including the `chat <model>` span name and `CLIENT` kind. Verify: `TestGenAI_AttributesMatchSemconv` + `TestGenAI_SpanNameMatchesSpec` + `TestGenAI_SpanKindClient`.
- A3. slog→OTel bridge preserves byte-equal local stream. Verify: `TestBridge_PreservesObsEventNames` + the existing `*_obs_test.go` corpus passes unchanged.
- A4. Trace-ID column populated on every relevant insert path. Verify: `TestWorkItemInsert_PersistsTraceIDFromContext` + `TestApprovalEvent_PersistsTraceIDFromContext`.
- A5. Operator doc covers every env var, the dev-stdout flag, the docker-compose fixture, the sensitive-payload policy. Verify: `TestObservabilityDoc_DocumentsAllEnvVars`.
- A6. `Config.Tracer` injection pattern uniform across all 8 components (one pattern, no method-overload drift). Verify: `grep -RnE 'Tracer\s+trace\.Tracer' internal/ | wc -l` returns 8 + `grep -RnE 'WithTracer\(' internal/ | wc -l` returns 0. Pins `feedback_spec_pattern_authority`.
- A7. Every named-but-deferred sub-decision filed as a tracking issue with title prefix `[W6-followup]`. Verify: `gh issue list --label W6-followup` lists ≥ 5 issues (metrics, multi-tenant tenant_id, sensitive-payload redaction, sampling-policy extension, claude SDK direct-call site).

### A+ (stretch — exceptional)

- A+1. A + the E2E test passes against the docker-compose Jaeger fixture in CI. Verify: `go test -tags e2e_otel ./internal/obs/otel/...` exit 0 in CI.
- A+2. Static lint test asserts goroutine context propagation discipline (`TestGoroutineCtxPropagation_LintCheck`). Verify: `go test -run TestGoroutineCtxPropagation_LintCheck`.
- A+3. Property test sweeps 200 synthetic trace shapes (ParentBased sampler decisions, attribute cardinality up to the SDK cap, span-link counts) and asserts no SDK panics, no goroutine leaks. Verify: `make property-test` exit 0 and `goleak.VerifyNone` clean.
- A+4. Performance baseline: TestBench in `internal/obs/otel/bench_test.go` shows < 5% scheduler-tick overhead with bridge handler installed (compared to MVP-2 baseline). Verify: `make bench` shows BenchmarkTick delta ≤ 5%.
- A+5. The exact span hierarchy and GenAI attr set is mirrored by a runnable docker-compose demo whose README screenshot shows a real Jaeger trace tree. Verify: PR body includes the screenshot + `examples/observability/README.md` link-checks.

---

## §8 File-disjoint task breakdown (the parallel-dispatch table)

Copy this table into the Wave 0 dispatch prompt. Each task is owned by one implementer subagent; the listed Path slice is its exclusive write scope. Tests for a task live under the task's package.

| Task | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| T1 SDK init | impl-1 | `internal/obs/otel/setup.go`, `internal/obs/otel/setup_test.go`, `internal/obs/otel/config.go` | — | M |
| T2 slog bridge | impl-2 | `internal/obs/otel/bridge.go`, `internal/obs/otel/bridge_test.go` | T1 (api only) | S |
| T3 migration + state | impl-3 | `internal/orchestrator/state/migrations/0005_trace_id_columns.sql`, `internal/orchestrator/state/work_items.go` (insert paths only), `internal/orchestrator/state/approvals.go` (insert paths only), `internal/orchestrator/state/trace_id_test.go` | — | S |
| T4 GenAI parser | impl-4 | `internal/orchestrator/spawner/genai.go`, `internal/orchestrator/spawner/genai_test.go`, `internal/orchestrator/spawner/testdata/stream-json/*.jsonl` | T1, T5 | M |
| T5 tracer injection | impl-5 | `internal/orchestrator/orchestrator.go` (Config.Tracer field + nil-fallback + tick span), `internal/orchestrator/scheduler/scheduler.go` (same), `internal/orchestrator/spawner/spawner.go` (operator_invocation span), `internal/orchestrator/reaper/reaper.go` (reap span), `internal/orchestrator/adaptersync/adaptersync.go`, `internal/gates/approval/gate.go` (gate.evaluate span), `internal/program/brief_loader.go`, `internal/orchestrator/adapter/markdown.go`, plus their `*_test.go` siblings for tracer-coverage tests | T1 | M |
| T6 E2E + docker-compose | impl-6 | `examples/observability/docker-compose.yml`, `examples/observability/README.md`, `internal/obs/otel/e2e_test.go` (build-tag `e2e_otel`) | T1, T2, T4, T5 | M |
| T7 Operator doc | impl-7 | `docs/operator/observability.md`, `docs/operator/observability_test.go` (link + env-var coverage) | T1, T6 | S |

Inter-task seam contracts (load-bearing — implementer MUST honour exactly):

- T1 exports `Setup(ctx, cfg) (shutdown, err)`, `Config{DevStdout bool}`, sentinel `ErrOTelExporterConflict`. T2/T3/T4/T5/T6 import these and only these.
- T5 adds `Config.Tracer trace.Tracer` to each component's Config struct. The field type is `go.opentelemetry.io/otel/trace.Tracer` — NOT a wrapper interface. Per `feedback_spec_pattern_authority`.
- T3 exports `state.PersistTraceIDFromContext(ctx, row *<Row>)` as the single seam every insert path uses to populate `trace_id`. T4 and T5 call it; do not reach into `trace.SpanContextFromContext` directly outside the obs/state packages.
- T4 imports `internal/obs/otel` only for the `trace.Tracer` API surface; the GenAI attr keys are constants in `internal/obs/otel/genai_attrs.go` (owned by T1) so T4 and any future direct-SDK call site share one source.

---

## §9 Risk preemption (adversarial red-team)

### R1 — Cardinality explosion in span attributes

**Threat**: `gen_ai.request.model` is bounded (Anthropic model SKU list), but operator-extended attrs via `OTEL_RESOURCE_ATTRIBUTES` or future per-message attrs could explode the backend's index.
**Mitigation**: §2 Out-of-scope keeps `gen_ai.input.messages` / `gen_ai.output.messages` off by default. Operator doc names the OTel SDK's `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` env var as the operator-side enforcement knob. Cardinality on regatta-emitted attrs is bounded by the spec table in §3.4.
**Verify**: `TestGenAI_AttributesMatchSemconv` — table-driven; any attr not in the table is a spec violation.

### R2 — OTLP backend down does not block app startup

**Threat**: Operator sets `OTEL_EXPORTER_OTLP_ENDPOINT` to a misconfigured / down endpoint. The exporter would block the orchestrator at startup.
**Mitigation**: OTel SDK's `WithBatcher` is fire-and-forget by design (the documented OTel-Go template behaviour). Exporter failures surface as dropped-span counters via the SDK's self-diagnostic, not as application errors. `Setup` returns nil error even when the exporter is later unreachable.
**Verify**: `TestSetup_OTLPEndpoint_BackendDown_StartupSucceeds` — point at `localhost:1` (unreachable); Setup returns nil; one Tick completes successfully; shutdown returns the export error joined per the OTel template.

### R3 — Trace context lost across goroutine boundaries

**Threat**: Scheduler tick spawns work via `go func() { ... }()`. Naive impl closes over the tick-scoped ctx and the span ends before the goroutine logs against it (use-after-end UB), or worse, captures *no* ctx and the goroutine logs against a noop tracer.
**Mitigation**: AST-walking lint test `TestGoroutineCtxPropagation_LintCheck` (A+ rubric A+2). Every `go func(...) {...}(...)` invocation that uses `tracer.Start` or `slog.<level>` inside must take the ctx as an explicit argument, never close over it. The test scans the diff and fails CI.
**Verify**: A+2 entry above.

### R4 — Key dependency churn (OTel SDK major-version migrations)

**Threat**: OTel Go SDK has shipped 2 major-version migrations historically. A future v2 migration could ripple through 8 components.
**Mitigation**: All SDK touchpoints live in `internal/obs/otel/`. Component code imports only `go.opentelemetry.io/otel/trace` (the stable, semver-locked subpackage). The setup file is < 300 LoC — a future migration is a single-file rewrite, not a cross-cutting rip. Vendored at `v1` per `go.mod`; bot-managed renovate PRs flag SDK bumps.
**Verify**: `grep -RnE 'go\.opentelemetry\.io/otel/sdk' internal/ | grep -v internal/obs/otel/` returns zero matches. Pins the encapsulation boundary.

### R5 — slog→OTel double-export risk

**Threat**: An operator's existing slog sink emits to stderr; the bridge ALSO emits to OTel logs; a downstream OTel Collector then routes back to stderr-as-logs — log records duplicated.
**Mitigation**: The bridge is multi-handler fan-out (slog primary unchanged, OTel-bridge handler additional). Operator doc explicitly states: "If your Collector routes OTel logs back to stderr, configure the Collector to drop service.name=regatta logs OR run with the stderr handler disabled." Documented limitation per `feedback_unaddressed_load_bearing`.
**Verify**: Operator doc §"Avoiding double-export" section + reference link to OTel Collector filter processor.

### R6 — Sampler-config trap

**Threat**: Operator sets `OTEL_TRACES_SAMPLER=traceidratio` with ratio 0.001; regatta's cost-governor reconciliation cron then sees 0.1% of LLM calls in traces and divides by sample ratio incorrectly → wrong invoice.
**Mitigation**: Cost-governor reconciliation reads token counts from the slog event stream (which is unsampled — slog records always go through the bridge unfiltered) AND uses spans only for *correlation*. Cost numbers do not depend on sample ratio. Documented as a contract between W6 (this) and the cost-governor wedge.
**Verify**: Cross-wedge contract documented in `docs/operator/observability.md` §"Sampling and cost reconciliation". Cost-governor spec carries the dual-source claim and a `TestCostReconciliation_SurvivesSampling` regression test (filed in W6-followup tracking issue, owned by cost-governor wedge).

### R7 — Sensitive-payload-on-span hazard

**Threat**: Future contributor adds `gen_ai.input.messages` "just to debug one trace" and ships secrets to the operator's observability backend.
**Mitigation**: §2 Out-of-scope is the design-time bar. `TestGenAI_SensitivePayloadNotEmitted` (T4) is the runtime gate — it lists the forbidden attrs explicitly and fails if any appears on any span.
**Verify**: T4 test row above.

### R8 — Replay-time span replay correctness (cross-wedge with W9)

**Threat**: W9 replay reads the journal AND the trace store, but a replayed `work_item` opens a span under a NEW trace_id — the original trace_id in the row no longer points at the replayed lifecycle.
**Mitigation**: Out-of-scope here; W9 spec will define replay-span semantics (new trace_id + span link to original, OR rewrite the row's trace_id with replay-mode flag). Tracking issue `[W6-followup] replay-time trace_id semantics` filed at impl-time. Per `feedback_unaddressed_load_bearing`.
**Verify**: W6-followup issue exists; A7 rubric counts it.

### R9 — Multi-tenant tenant_id propagation contract with W8

**Threat**: W8 introduces `tenant_id` at every read path. If W6 lands without a `regatta.tenant_id` resource attribute, W8 has to retrofit every span.
**Mitigation**: §3.1 Setup hardcodes `regatta.tenant_id=default` at the resource level. W8 swaps the constant for a per-context lookup; the change is one line. Tracking issue `[W6-followup] tenant_id propagation contract with W8` filed at impl-time.
**Verify**: `TestSetup_ResourceCarriesServiceNameAndTenant` + W6-followup issue.

### R10 — Spawner subprocess never emits stream-json (legacy operator config)

**Threat**: If operator runs `claude` without `--output-format=stream-json`, no `llm_call` span ever opens — cost-governor reconciliation silently fails.
**Mitigation**: Operator doc bolds "REQUIRED: pass `--output-format=stream-json` in `regatta.yaml: spawner.args`." Validator at config-load time fails closed if `spawner.kind=claude` AND `--output-format=stream-json` is absent from args.
**Verify**: `TestConfig_ClaudeSpawner_RequiresStreamJsonForObservability` — config validator rejects the bad combo. Filed against T7 (operator-doc PR carries the validator commit alongside the doc).

---

## §10 Wave breakdown

Three waves, file-disjoint within each wave. Each wave clears `make check` and adversarial-reviewer subagent before the next dispatches per `feedback_adversarial_review`.

### Wave 1 — Foundations (T1 + T2 + T5)

**T1, T2, T5 dispatched in parallel.** T1 owns the SDK setup. T2 owns the slog bridge (depends on T1's exported `Config` type only — both subagents start simultaneously because the API surface is pinned in this spec). T5 owns Config.Tracer injection across 8 components (depends on T1's `trace.Tracer` type — which is `go.opentelemetry.io/otel/trace`, available the moment go.mod is updated).

Wave 1 exit gate: every existing component compiles + boots with a tracer field, the SDK setup works against a stub OTLP listener, the bridge fans every record correctly. No new span attrs yet (that's Wave 2).

### Wave 2 — Data plane (T3 + T4)

**T3 + T4 dispatched in parallel.** T3 lands migration 0005 and state-op writers. T4 lands the GenAI semconv attribute set on `llm_call` spans via the stream-json parser. Both consume Wave 1 outputs; neither touches the other's files.

Wave 2 exit gate: a docker-compose-less unit test pipes a stream-json fixture through the spawner and asserts the full `tick → work_item → operator_invocation → llm_call` span tree carries the GenAI attrs. `work_items.trace_id` and `approval_events.trace_id` populate on insert.

### Wave 3 — E2E + docs (T6 + T7)

**T6 + T7 dispatched in parallel.** T6 lands the docker-compose Jaeger fixture and the build-tag-gated E2E test. T7 lands the operator doc. Both depend on Waves 1+2.

Wave 3 exit gate: E2E test green in CI under the `e2e_otel` tag; operator doc passes `make doc-check`. Adversarial reviewer subagent attests no unaddressed findings. PR body release-notes block category `[FEATURE]`.

After Wave 3 merges: file the W6-followup tracking issues (metrics, sensitive-payload redaction, multi-tenant contract with W8, replay semantics with W9, direct-SDK call site) per A7 rubric.

---

## Appendix A — Adopted-OSS dependency manifest (cite-by-version)

```
go.opentelemetry.io/otel                                        v1.x  (semver-locked at major v1)
go.opentelemetry.io/otel/sdk                                    v1.x
go.opentelemetry.io/otel/trace                                  v1.x
go.opentelemetry.io/otel/log                                    v0.x  (stable for the log signal; tracking promotion)
go.opentelemetry.io/otel/sdk/log                                v0.x
go.opentelemetry.io/otel/log/global                             v0.x
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.x
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc     v0.x
go.opentelemetry.io/otel/exporters/stdout/stdouttrace           v1.x
go.opentelemetry.io/otel/exporters/stdout/stdoutlog             v0.x
go.opentelemetry.io/otel/semconv/v1.41.0                        v1.41.0  (cite the schema URL exactly)
go.opentelemetry.io/contrib/bridges/otelslog                    v0.19.0+
```

Renovate bot manages the bumps. Major SDK migration → R4 mitigation applies.

---

## Appendix B — Why each design choice picked OTel over bespoke

| Choice | Bespoke option considered | Why OTel won |
|---|---|---|
| Tracer field on Config | regatta-defined `Tracer` interface | OTel's `trace.Tracer` IS the interface; wrapping it adds a layer with no semantic value and breaks dashboards that import OTel types directly. Adopt verbatim. |
| GenAI attr keys | `obs.AttrKeyLLMTokensInput` etc. | OTel GenAI semconv is the ratified industry shape (brief §4 W6 prior-art). Every backend ships dashboards keyed on these strings. Inventing parallel keys = lock-in + dashboard rewrites. |
| Exporter wiring | regatta-yaml `observability.endpoint` | OTel env vars are the canonical contract every backend documents against. Operators already know them. (Brief §5 thread #3 — swap-out.) |
| Slog bridge | hand-rolled handler that calls into OTel logs | `otelslog.NewHandler` is upstream-maintained, attaches TraceID + SpanID automatically (verified in fetched docs), and is the documented adoption path. Bespoke would re-derive the SpanContextFromContext pull and re-implement severity mapping. Adopt verbatim. |
| Sampler | bespoke "sample 1-in-N tick spans" | `sdktrace.ParentBased(AlwaysSample())` default + operator-overridable via `OTEL_TRACES_SAMPLER` per the SDK spec. Bespoke would lose the parent-respecting semantics every backend assumes. |

---

_End of spec. Total line count target: ≤ 900 (this file: ~610). Spec freezes the W6 pattern per `feedback_spec_pattern_authority`; implementer-subagent deviations require re-spawning this subagent._

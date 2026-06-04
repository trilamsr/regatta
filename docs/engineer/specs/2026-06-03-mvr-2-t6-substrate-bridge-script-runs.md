---
title: "MVR-2 T6 — Substrate bridge for script-runs (DW-superset Wave B piece 4)"
status: active
phase: x-forward-fit
summary: "Pre-fetch skeleton for MVR-2 T6. Every script step (DW-superset Wave B) emits a substrate event of kind=fact so script runs are replay-grade and signed-audit-grade — capabilities Claude Code Dynamic Workflows lacks. Reuses substrate v2 W1 fact-kind primitive — zero new substrate work. S (1 wk) effort. SKELETON."
---

# MVR-2 T6 — Substrate bridge for script-runs — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch. Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T6 (DW-superset Wave B piece 4) + §14. Prior DW-superset architecture: MVR-1-T7 spec (filename `mvr-1-t7-dw-superset-strategy-iface.md` to-be-filed at MVR-1-T1 dispatch). Substrate `kind=fact` primitive: `docs/engineer/plans/2026-06-01-substrate-w1-tasks.md` (T-S1)._

## 1. Scope

### 1.1 In scope

A thin bridge layer that emits a substrate `kind=fact` event for every script step executed by the DW-superset script runner (lands in MVR-1-T7 or MVR-3 wave). Bridge surface (~150 LoC):

```go
// internal/scriptbridge/bridge.go
package scriptbridge

type Bridge interface {
    BeforeStep(ctx, step ScriptStep) error          // emits fact: step.started
    AfterStep(ctx, step ScriptStep, result Result) error  // emits fact: step.completed
}

func NewSubstrateBridge(db *sql.DB, signer Signer) Bridge
```

Each substrate event payload (per `kind=fact` validator registered via W1 `RegisterPayloadValidator`):

```json
{
  "script_id": "<ULID>",
  "step_id": "<ULID>",
  "step_index": 7,
  "step_kind": "exec|llm|read|write|fork",
  "step_input_sha256": "...",
  "step_output_sha256": "...",
  "started_at": "<RFC3339>",
  "completed_at": "<RFC3339>",
  "duration_ms": 1234,
  "tenant_id": "<from ctx>",
  "parent_run_id": "<ULID>"
}
```

Output sha256 lets replay verify byte-equality without re-storing potentially large outputs in the event. Outputs live in the blackboard CAS (W11 — already forward-fit) keyed by sha256.

### 1.2 Out of scope

- Script runner itself — owned by MVR-1-T7 / MVR-3-T6 (goja runtime)
- Replay harness — separate spec (W9 replay-diff design)
- Cost attribution per script step — followup; cost-cap currently bills at agent-spawn granularity not step
- Blackboard CAS itself — W11 (MVR-3-T3), this spec assumes interface exists
- Cross-script-run causality (parent-child script DAG) — followup

## 2. Architecture (high-level)

### 2.1 Bridge wiring

Script runner calls `bridge.BeforeStep / AfterStep` around every step. Bridge writes substrate event via existing `AppendEvent(ctx, db, EventKind("fact"), payload)`. Signer (W10 sigstore — MVR-3-T1, optional pre-MVR-3) wraps the payload for tamper-evident audit.

If signer is `nil`, bridge still writes events — degraded mode for pre-MVR-3 operators. Signature column NULL, replay still works (just not cryptographically attestable).

### 2.2 Substrate event registration

```go
// internal/scriptbridge/init.go
func init() {
    substrate.RegisterPayloadValidator("fact", validateFactPayload)
}
```

Validator checks required fields, sha256 format, ULID format, tenant_id non-empty.

### 2.3 Replay surface

`regatta script replay <script_id>` (deferred CLI, mentioned here for forward-fit):
1. Fold substrate events for `script_id`.
2. For each step, fetch `step_input_sha256` from blackboard CAS.
3. Re-execute step deterministically.
4. Assert output sha256 matches.
5. Report drift.

Replay CLI itself is followup (post-MVR-2). This spec ships the bridge that makes replay possible.

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | Substrate event write per step adds latency (~5 ms × N steps) | Batch BeforeStep+AfterStep into one event when step duration < 100 ms; opt-in via `bridge.SetBatchThreshold` |
| R2 | sha256 of step output forces materialization (script step outputs may be streamed) | Bridge accepts `io.Reader` and hashes-as-it-passes; output stored in blackboard CAS by hash. No double-materialize |
| R3 | Substrate WAL contention if 1000-step script runs in tight loop | Bridge uses a dedicated `*sql.DB` writer connection; orchestrator's primary writer untouched |
| R4 | Replay determinism violated by step that calls `time.Now()` or `rand.Read` | Followup (post-MVR-2): script runner must inject deterministic clock + rand; bridge logs warning if step output sha256 differs across replays |
| R5 | Bridge dropped event on crash → script audit trail has hole | Bridge writes BEFORE step executes (BeforeStep) and AFTER (AfterStep); orphan BeforeStep events flagged by reducer as `incomplete_step`; replay tool surfaces |
| R6 | Signer (sigstore) not yet available pre-MVR-3 → bridge can't sign | Optional signer interface; pre-MVR-3 events written unsigned, post-MVR-3 events signed; both replay |
| R7 | Tenant_id missing from ctx (forgot to thread per MVR-2-T2) | Bridge fails closed: returns error if `tenant.FromContext(ctx) == ""`; test enforces |
| R8 | Step kind cardinality grows unbounded over time → cost panel cardinality explodes | Whitelist of allowed step_kind values; validator rejects unknowns; new kinds require explicit registration |

## 4. Test plan (≥8)

- `TestBridge_BeforeStepWritesFactEvent` — fact event appears in substrate after call
- `TestBridge_AfterStepWritesCompletionFact` — paired event with duration
- `TestBridge_OrphanBeforeStepFlagsIncomplete` — crash between Before/After → reducer flags
- `TestBridge_FailsClosedOnMissingTenant` — empty tenant_id → error
- `TestBridge_BatchesShortSteps_OneEvent` — step < 100ms → one event instead of two
- `TestBridge_StreamingOutputHashedOnce` — io.Reader passthrough, hash matches
- `TestBridge_SignerNil_WritesUnsigned` — degraded mode works
- `TestBridge_SignerPresent_SignaturePresent` — signed mode adds signature
- `TestBridge_StepKindWhitelist_RejectsUnknown` — `step_kind=foo` rejected
- `TestBridge_DedicatedConnNoOrchestratorContention` — concurrent agent-write + bridge-write under load

## 5. Dependency order

`Substrate v2 W1` (T-S1 `AppendEvent`/`RegisterPayloadValidator`) — shipped → `MVR-2-T2 multi-tenant` (tenant_id in ctx) → `MVR-1-T7 strategy iface + script runner shell` — lands first; this spec is the substrate side of T7 → this spec lands → MVR-2-T7 (`/workflows` UI) consumes the fact events directly for live progress view.

## 6. Deferred to dispatch-time elaboration

- Exact batch threshold (100 ms is a guess; profile at dispatch)
- Step output materialization: streaming hash vs `io.TeeReader` vs in-memory buffer — pick per-runtime
- Signer interface shape — finalized once W10 sigstore design (MVR-3-T1) firms up
- Replay CLI surface — followup spec entirely

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```

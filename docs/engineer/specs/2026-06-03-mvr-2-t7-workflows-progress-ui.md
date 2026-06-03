---
title: "MVR-2 T7 — /workflows progress UI (DW-superset Wave A piece 6)"
status: active
summary: "Pre-fetch skeleton for MVR-2 T7. Read-only `/ui/workflows/{script_id}` surface that streams live step-by-step progress of a script run. Reuses W7 htmx scaffold (Wave 1/2). Reads substrate fact events written by T6 substrate bridge. S (1-2 wk) effort. SKELETON."
---

# MVR-2 T7 — `/workflows` progress UI — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch. Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T7 (DW-superset Wave A piece 6) + §14. Consumes T6 substrate bridge fact events. Reuses W7 htmx scaffold (Wave 1/2)._

## 1. Scope

### 1.1 In scope

One new read-only UI surface mounted on the existing `regatta serve --ui-addr` listener:

| # | Surface | Route | Upstream data | Cadence |
|---|---|---|---|---|
| W1 | Workflow list | `GET /ui/workflows` | Substrate `events WHERE kind='fact' AND payload.step_kind IS NOT NULL` grouped by script_id | poll 5 s |
| W2 | Workflow detail | `GET /ui/workflows/{script_id}` | Substrate fact events for one script_id, ordered by step_index | poll 2 s while running, 30 s after completion |
| W3 | Step output viewer | `GET /ui/workflows/{script_id}/step/{step_id}` | Blackboard CAS read by `step_output_sha256` | on-demand (no polling) |

All surfaces reuse:
- Wave-1 listener (`internal/web/ui/handler.go`)
- Wave-1 templates (`internal/web/ui/templates/`)
- Wave-1 dedicated read-only sqlite WAL connection

### 1.2 Out of scope

- Script invocation / kill / restart — CLI only (same constraint as W7 Wave-1)
- Live streaming via SSE — Phase X
- Per-step logs (stdout/stderr) — followup; v1 shows step input/output sha256 + duration only
- Script editor / authoring — explicitly out (DW positions itself there; regatta differentiator is the audit trail, not the IDE)
- Multi-script DAG view (parent-child script causality) — followup

## 2. Architecture (high-level)

### 2.1 Workflow list (W1)

Substrate query (folded result cached 5 s in-process):

```sql
SELECT
  payload->>'script_id' AS script_id,
  COUNT(*) AS step_count,
  MAX(ts) AS last_event_ts,
  MAX(payload->>'completed_at') IS NULL AS in_flight
FROM events
WHERE kind = 'fact'
  AND payload->>'step_kind' IS NOT NULL
  AND tenant_id = ?
GROUP BY payload->>'script_id'
ORDER BY last_event_ts DESC
LIMIT 50
```

Empty state: `no scripts run yet`. Single panel, no per-script polling overhead.

### 2.2 Workflow detail (W2)

Server-side render of an ordered table of steps. Each step row shows:
- step_index, step_kind, duration_ms, started_at
- input_sha256 (link to W3), output_sha256 (link to W3), or "incomplete" if AfterStep missing
- inline icon for signer status (signed / unsigned / signature-invalid)

Polling cadence: 2 s while at least one step is `incomplete`, 30 s otherwise. Adaptive `hx-trigger` swapped on the fly.

### 2.3 Step output viewer (W3)

Fetches blackboard CAS object by sha256. Pre-renders:
- If content-type detected as JSON: pretty-print with line numbers
- If text/plain: pre-wrapped
- If binary: shows hex dump head + offer `regatta script cat <sha256>` CLI command

CAS read budget: <5 MB inline; larger objects redirect to CLI.

### 2.4 Substrate event subscription

Same pattern as Wave-2 DAG view: subscribe to internal event bus for `kind=fact` writes, invalidate the per-script cache. TTL bounds staleness if subscription drops.

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | Script with 10k steps blows out the detail table HTML | Virtual-scroll via `<table>` slicing — render first 200 rows + pagination link; full-table fallback to CLI `regatta script trace` |
| R2 | Adaptive polling cadence flips rapidly (steps flapping `incomplete` → `complete` → spawn new) | Hysteresis: 5 consecutive polls without `incomplete` → drop to slow cadence; one incomplete → fast immediately |
| R3 | CAS read of 5 MB object on UI thread blocks the listener | Stream from CAS with explicit size cap; reject >5 MB at handler with redirect message |
| R4 | Step output content-type sniff misidentifies binary as text → renders garbage | Use `http.DetectContentType` (Go stdlib) — same heuristic browsers use; conservative on UTF-8 validity |
| R5 | Cross-tenant script view leaks scripts from other tenants | Query includes `tenant_id = ?` from W7 Principal; same guard as DAG view (Wave-2) |
| R6 | Signature-invalid step rendered as "signed OK" because the verify call timed out | Verify call has 100ms budget; on timeout render `signature unknown` not `signed` (fail-closed UX) |
| R7 | Workflow list query `payload->>'script_id'` does full table scan on 10M-event substrate | Partial index on `events(payload->>'script_id') WHERE kind='fact'` ships with this spec |
| R8 | Pagination state lost on `hx-swap` refresh → operator scrolls back to top every 2s | `hx-preserve` on pagination control OR offset in URL fragment |

## 4. Test plan (≥8)

- `TestWorkflows_ListGroupsByScriptID` — fixture with 3 scripts, list shows 3 rows
- `TestWorkflows_DetailOrdersByStepIndex` — fixture with out-of-order events, render is sorted
- `TestWorkflows_DetailFlagsIncompleteOnOrphanBefore` — orphan BeforeStep → row shows incomplete
- `TestWorkflows_DetailAdaptivePolling_FastWhileInFlight` — polling interval transitions
- `TestWorkflows_StepViewer_PrettyPrintsJSON` — JSON object renders pretty
- `TestWorkflows_StepViewer_LargeObjectRedirectsToCLI` — >5 MB → redirect message
- `TestWorkflows_TenantScoped_NoLeakAcrossTenants` — Principal.Tenant filter holds
- `TestWorkflows_SignatureUnknownOnVerifyTimeout` — slow verify → unknown state, not signed
- `TestWorkflows_PartialIndexUsed` — EXPLAIN QUERY PLAN shows index scan on workflow list
- `TestWorkflows_PaginationPreservesOffset_OnHxSwap` — refresh keeps scroll position

## 5. Dependency order

`MVR-1-T1 W7 Wave 1` (UI scaffold + WAL conn) — shipped or in-flight at MVR-2 dispatch → `MVR-2-T2 multi-tenant` (tenant_id filter) → `MVR-2-T6 substrate bridge` (fact events to consume) — lands first → this spec lands → MVR-2-T5 (Wave-3 polish) consumes this surface for the polished doc.

## 6. Deferred to dispatch-time elaboration

- Exact pagination size (200 row default; tune at dispatch on fixture data)
- Adaptive-polling hysteresis tuning (5 polls is a guess)
- Step kind icon set (font-awesome vs heroicons vs SVG inline) — pick at dispatch
- CAS read budget — re-evaluate once W11 blackboard ships with real size distribution
- Partial-index migration number — pinned at dispatch time per `feedback_migration_number_lock`

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```

---
title: "MVR-2 T1 — W7 Wave 2 htmx (DAG read view + reviewer-rich PR UI)"
status: active
phase: x-forward-fit
summary: "Pre-fetch skeleton spec for MVR-2 T1. Extends MVR-1 T1 W7 Wave 1 dashboard with two new read surfaces: agent-DAG visualization and reviewer-comment-aware PR detail view. Still read-only (write paths stay CLI per Wave 1 constraint). Vendored htmx 2.0.x + Pico CSS. 3-4 wk effort band. SKELETON — full elaboration at MVR-2 dispatch time."
---

# MVR-2 T1 — W7 Wave 2 htmx UI (DAG + reviewer surfaces) — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch time per `feedback_design_iteration_local` (no LOC committed until phase activates). Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T1. Prior Wave-1 spec: `docs/engineer/specs/2026-06-02-mvr-1-t1-w7-wave1-htmx-ui-mvp.md`. Parent W7 design: `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`._

## 1. Scope

### 1.1 In scope (Wave 2)

Two new read-only surfaces grafted onto the Wave-1 dashboard listener (`regatta serve --ui-addr`):

| # | Surface | Route | Upstream data | Cadence |
|---|---|---|---|---|
| S1 | Agent-DAG view | `GET /ui/dag/{run_id}` | substrate `events WHERE run_id=?` folded into parent-child agent tree | poll 10 s |
| S2 | Reviewer-rich PR detail | `GET /ui/pr/{pr_number}` | `gh pr view --json` + W7-Wave3-reviewer-comments cache | poll 10 s |

Both surfaces:
- Reuse Wave-1 templating stack (`internal/web/ui/templates/`).
- Reuse the dedicated read-only sqlite WAL connection (`internal/web/ui/conn.go`).
- No write paths. Mutations still CLI-only — same Wave-1 constraint that drops the auth question.
- htmx polling only. No SSE (deferred to Phase X per Wave-1 §2.3).

### 1.2 Out of scope (still)

- Write paths (approve/reject/reset) → owned by Wave-1 spec §2.3 + W7 parent design surface #1.
- DAG mutation / re-run agent → CLI only.
- Mobile-optimized layout → Wave-3 (T5).
- Per-handler OTel spans → Wave-3 (T5).
- Auth / per-tenant filtering → T2 (W8 OPA RBAC) lands first; Wave-2 reuses W7 Principal seam.

## 2. Architecture (high-level)

### 2.1 DAG view

Substrate query: agent fork events (`kind=agent_spawned`) form parent→child edges keyed on `run_id`. The fold builds an in-memory `*Node` tree, depth-first rendered into nested `<details>` blocks (semantic HTML; Pico CSS styles tree out of the box). Pure server-side rendering — no client-side graph library. Estimated render budget at p99 (100-agent run): <30 ms. Tree depth empirically ≤6 (per substrate event audit week 4 of MVP-3); deeper trees fall back to flat list.

Caching: per-`run_id` 10 s in-memory cache (`sync.Map` keyed on run_id). Cache invalidated by a `substrate_event_committed` channel subscription (existing internal event bus from MVP-2 W2). If subscription drops, TTL still bounds staleness.

### 2.2 Reviewer-rich PR detail

Reads `gh pr view --json comments,reviews,reviewThreads` (or equivalent SCM adapter per MVR-1-T5 once that lands) on 10 s polling cadence per active PR. Renders comments grouped by reviewer identity, threaded by `in_reply_to_id`. Filter: hide comments older than the latest PR push (operator-grade default; toggle persists in cookie, not server-side).

Critical: the upstream `gh pr view` call MUST route through the SCM adapter (MVR-1-T5) by the time Wave-2 lands. If MVR-1-T5 ships first (per roadmap §4 sequencing), this spec consumes it directly; if MVR-1-T5 slips, Wave-2 ships against `gh` and migrates in Wave-3. Dispatch-time decision.

### 2.3 Listener wiring

Same `*http.Server` from Wave-1. New routes mounted on the existing mux: `/ui/dag/`, `/ui/pr/`. Static assets (`htmx.min.js`, `pico.min.css`) already vendored.

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | DAG render blows the 5s polling budget on 1000-agent runs | Cap tree render at 200 nodes; show `+N truncated` link to CLI `regatta dag --run <id>` |
| R2 | `gh pr view` rate-limit on a 10-PR active surface (60 req/min vs GH 5000/hr) | Per-PR cache + jitter (3-13 s actual interval). Adapter migration (MVR-1-T5) gains native API token allowance |
| R3 | Substrate event subscription lag → DAG shows stale parent-child edges | TTL fallback (10 s) bounds staleness; e2e test asserts edge appears within 2× TTL |
| R4 | Comment thread fold mis-attributes reviewer when GH renames user mid-thread | Pin reviewer identity to `user.node_id`, not login; golden test on rename event fixture |
| R5 | XSS via reviewer comment body (markdown not sanitized) | Pipe through `bluemonday` strict policy at render; CSP `script-src 'self'` from Wave-1 already blocks inline scripts |
| R6 | Memory leak on long-running listener (cache never evicts on completed runs) | LRU cap at 64 runs in the DAG cache; eviction test under `internal/web/ui/cache_test.go` |
| R7 | Race between polling refresh and run completion → 404 flicker | `hx-target` swaps in-place; missing run renders `RUN_COMPLETED` empty-state, NOT 404, so the polling loop stops cleanly |
| R8 | DAG visual depth degrades on mobile (<480 px) | Pico CSS `<details>` collapses by default at narrow viewport; explicit `viewport-fit` meta — acceptable until Wave-3 mobile pass |

## 4. Test plan (≥8)

- `TestDAG_ForestFromEvents_Depth6` — fold synthetic 6-level agent tree from substrate fixture
- `TestDAG_TruncatesAt200Nodes_LinkRendered` — large-run cap behavior
- `TestDAG_CacheInvalidatesOnSubstrateEvent` — channel-driven invalidation roundtrip
- `TestDAG_RenderBudget_P99Under30ms` — micro-benchmark gate, fail at 50 ms
- `TestPRDetail_GroupsByReviewerNodeID_NotLogin` — rename-fixture golden test
- `TestPRDetail_FilterHidesPrePush_DefaultsOn` — cookie-driven filter persistence
- `TestPRDetail_BluemondaySanitizesScript` — XSS smoke test
- `TestPRDetail_GhFailureRendersEMPTYState_NotErrorPage` — `gh` missing on PATH degrades gracefully (Wave-1 contract)
- `TestUI_DAGAndPR_BothPollIndependently` — handler isolation; one stuck handler doesn't block the other
- `TestUI_ListenerHandlesDAGPR_NoRoute Collision` — mux ordering regression guard

## 5. Dependency order

`MVR-1-T1 W7 Wave 1 (#601)` lands first (Wave-1 listener + templates + dedicated WAL conn) → `MVR-1-T5 SCM adapter` ideally lands second (Wave-2 PR detail consumes it) → `MVR-2-T2 W8 tenant_id` lands either before or in parallel (Wave-2 reuses Principal seam; if T2 slips, Wave-2 hard-codes `tenant="default"` and rewires on T2 land).

## 6. Deferred to dispatch-time elaboration

- Exact `internal/web/ui/dag.go` API surface
- Substrate event subscription channel buffering size + drop policy
- Per-tenant filtering on `/ui/dag/` once T2 lands
- Reviewer-comment markdown rendering rules (math? code blocks? mentions?)
- A11y pass on the `<details>` tree (screen-reader nav)

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```

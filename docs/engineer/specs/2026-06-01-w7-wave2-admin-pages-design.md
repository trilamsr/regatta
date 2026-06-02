# MVP-3 W7 Wave 7.2 — Admin DAG list + run detail pages (T8 + T9, design spec)

_Author: design subagent, 2026-06-01. Wave-extension of `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` (W1). Source-of-truth: that W1 spec §1 scope, §3.2 package layout, §3.6 auth, §3.10 data sources. Umbrella issue: [#183](https://github.com/trilamsr/regatta/issues/183)._

This spec extends Wave 7.2 of the W1 design with two operator-facing read-only admin surfaces:

- **T8** — `GET /runs/` — admin DAG list page (paginated, filtered, sortable).
- **T9** — `GET /runs/{run_id}` — run detail page (work-items tree + edge states + DAG topology).

Both pages are server-rendered, htmx-progressive, JS-free fallback. RBAC retrofits via W8 Authorizer once it lands. Until then: HMAC reviewer cookie (W7 W1 T7, PR #303) pass-through, single-tenant.

The Wave 1 spec's W7.2 entry (`internal/web/runs.go` single-handler sketch) is **superseded** by this document: T8 + T9 split work-items list from a separate admin index. T10 (Playwright E2E) lands unchanged per W1 §7.

---

## 1. Goal + non-goal

### 1.1 IN scope

1. `GET /runs/` — admin page listing recent DAG runs, newest first, paginated via opaque cursor (`?before=<cursor>`). Defaults to 50 rows.
2. `GET /runs/{run_id}` — detail page showing a tree of work_items + edge states + DAG topology, capped at 500 nodes per page with collapsible groups.
3. Read-only views; no edit/rerun/requeue affordances.
4. Operator-only authn:
   - W8-landed: RBAC via `Authorizer.Check(p, "run.view", "*")`.
   - Pre-W8: HMAC reviewer cookie pass-through (single-tenant assumption documented at §3.7).
5. Filtering (status × lane × free-text run_id substring) + sorting (`started_at`, `completed_at`, `status`, `lane`).

### 1.2 OUT scope (deferred)

- **NO edit / rerun / replay buttons** — W9 replay scope owns those.
- **NO cross-tenant aggregation** — W8 OPA RBAC gates that.
- **NO live polling beyond htmx `every 5s` + GET refresh** — no SSE, no WebSocket in v1.
- **NO graph visualization beyond sortable nested HTML list** — Wave 3 T12 graphviz/SVG render is the followup.
- **NO mobile-first layout.**
- **NO JS framework** (per W1 §2 prior-art table — htmx 2.0.4 only).
- **NO new third-party deps**; reuse `html/template` + vendored htmx.
- **NO new SQL migrations** — reads existing `work_items`, `work_item_edges`, substrate `events`.

## 2. In / Out (concrete)

| Surface | In v1 | Out of v1 | Notes |
|---|---|---|---|
| `/runs/` index | top 50 rows, opaque cursor, filter + sort | live diff stream, cross-tenant | Pagination via substrate-style opaque cursor (see §3.2). |
| `/runs/{run_id}` detail | work-item tree (≤500 nodes), edge list, DAG topology (collapsible) | full graphviz render, force-rerun | Cap on render footprint to fence R1. |
| RBAC | Authorizer.Check when W8 landed; HMAC reviewer cookie otherwise | OPA bundles, tenant scoping | Forward-compat at the `Principal` type level (W1 §3.6.4 + I4). |
| Polling | `hx-trigger="every 5s"` + `hx-sync` on detail page only; index page is GET-refresh only | SSE / WebSocket | Index page already paginated; polling collides with cursor. |
| Test depth | 6 named B + 3 A + 1 A+ property test per page | mutation testing | Property tests cover cursor roundtrip + render budget. |

## 3. Architecture

### 3.1 Routes

| Method | Path | Purpose | Auth | Caching |
|---|---|---|---|---|
| `GET` | `/runs/` | Admin DAG list. Renders top-N rows by `started_at DESC`. Cursor via `?before=<opaque-bytes>`. | HMAC reviewer cookie (W7 W1 T7 #303); W8 swap to `Authorizer.Check`. | `Cache-Control: no-store` |
| `GET` | `/runs/{run_id}` | Run detail page: work-item tree (≤500 nodes) + edge list + DAG topology summary. | same | `Cache-Control: no-store` |

Both routes mount under the existing `internal/web` mux from W1 T4 (PR #307). No new listener; no new prefix carve-out.

### 3.2 `/runs/` query plan

Top-50 newest DAG runs by `started_at` DESC. Pagination uses an opaque cursor; the substrate has the canonical encoding (per `docs/engineer/specs/2026-06-01-unified-substrate-design.md` §4 fold semantics on `(written_at, kind, work_item_id)` tuples). `internal/web/cursor.go` (T10 followup) wraps:

```go
// cursor encodes (started_at, run_id) for keyset pagination.
type Cursor struct {
    StartedAt time.Time
    RunID     string
}
func EncodeCursor(c Cursor) string  // base64(canon)
func DecodeCursor(s string) (Cursor, error)
```

v1 cursor encoding ships in T10 as a shared primitive owned by the index page. T8 uses an inline-encoded cursor (`base64(json{ts,id})`) until T10 lands; the surface is private (only T8 emits + parses, no external consumers), so the swap is a no-op upgrade.

Two SQL queries max per render (asserted via `dbtest.QueryCounter` from W7.0 T2, PR #255):

1. `SELECT run_id, started_at, completed_at, status, lane, item_count FROM runs_view WHERE (started_at, run_id) < (?, ?) ORDER BY started_at DESC LIMIT 51` — fetch 51 to detect `has_more` without a `COUNT(*)`.
2. (optional) `SELECT COUNT(*) FROM runs_view` — only on first page when no cursor, gated behind a feature flag; default off in v1 (preserves the ≤2 budget even when the optional row count fires).

If `runs_view` does not exist (substrate Phase 1 not applied), handler renders a "run index unavailable — substrate migration `0006_substrate.sql` not applied" banner, mirroring the cost-panel fail-soft pattern from W1 §3.9.

Open question: whether `runs_view` should be a materialized view or a `SELECT … FROM substrate_events GROUP BY run_id` fold. Resolved by W1 §3.10 reconciliation paragraph — fold over `substrate_events WHERE kind IN ('node_output','node_complete')` is the answer once Phase 3 ships. v1 reads the legacy `work_items` aggregate.

### 3.3 `/runs/{run_id}` query plan

Two SQL queries max, asserted via `QueryCounter`:

1. `SELECT id, lane, state, started_at, ended_at, trace_id, parent_id, depth FROM work_items WHERE run_id = ? ORDER BY depth ASC, started_at ASC LIMIT 501` — fetch 501 to detect overflow (cap = 500 per R1).
2. `SELECT from_id, to_id, edge_kind, traversed_at FROM work_item_edges WHERE from_id IN (SELECT id FROM work_items WHERE run_id = ?)` — single bulk fetch.

Tree shape derived in Go (no recursive CTE). `parent_id` + `depth` columns already exist on `work_items` (per existing migrations — verified by `grep -n "parent_id" internal/orchestrator/state/`).

If overflow (≥501 rows): truncate to 500 + render "+N more (showing first 500 — open question: pagination on detail-page tree)" banner. Tracking issue for paginated detail page filed pre-merge.

### 3.4 Templates

Both pages extend `templates/layout.tmpl` from W1 T4. New templates under `internal/web/templates/`:

| File | Type | Renders | Includes |
|---|---|---|---|
| `runs_list.tmpl` | page | top-N runs table + filter bar (status × lane × run-id substring) + sort headers + cursor pagination footer | `_flash.tmpl` from W1 T4 |
| `run_detail.tmpl` | page | header (run_id, started_at, status), filter sub-bar (state × lane), work-item tree (nested `<ul>`), edges list, topology summary | `_workitem_node.tmpl`, `_flash.tmpl` |
| `_workitem_node.tmpl` | partial | one work-item node + nested children; renders state badge + duration + trace_id link | recursive into itself |

The recursive `_workitem_node.tmpl` partial uses `template.New("node").ParseFS(...)` with bounded recursion depth via a render-time counter (passed in template data) so a cyclic edge cannot wedge the render. Cap = 500 traversals; over → stop + render "cycle truncated" indicator.

### 3.5 Filtering

Top bar (both pages) sets query-param state:

| Param | Values | Default | Notes |
|---|---|---|---|
| `?status=` | `pending`, `running`, `done`, `failed`, empty | empty (all) | Server-side `WHERE status = ?` if set. |
| `?lane=` | any non-empty string | empty | LIKE-free exact match. |
| `?q=` | free text | empty | Substring grep on `run_id` only. NOT on operator-controlled fields (XSS-vulnerable; see R6). |
| `?before=` | opaque cursor | empty (first page) | Validated via `substrate.DecodeCursor` (or local fallback). |

Filter form submits via plain `<form method=GET>`; no JS required.

### 3.6 Sorting

`?sort=` accepts `started_at`, `completed_at`, `status`, `lane`. `?dir=` accepts `asc`, `desc`. Server-side parameterized; whitelist-validated (any other value → 400 + flash banner, no SQL pass-through).

Default: `sort=started_at&dir=desc`.

### 3.7 Authn — pre-W8 vs post-W8

`web.Principal` from cookie HMAC ships in W7 W1 T7 (PR #303, about to merge per W1 §7 status). When W8 lands:

```go
// W7.2 handler signature locks now:
func (h *Handler) RunsList(w http.ResponseWriter, r *http.Request) {
    p, err := web.PrincipalFromRequest(r) // W7 W1 T7
    if err != nil { renderError(...) ; return }
    // Pre-W8: any authenticated reviewer passes.
    // Post-W8: authorizer.Check(p, "run.view", "*") gates here.
    if h.authz != nil {
        if err := h.authz.Check(p, "run.view", "*"); err != nil { renderError(...) ; return }
    }
    // ... render
}
```

`h.authz` is `nil` until W8 lands. Single-tenant assumption explicit in the handler docstring + `docs/engineer/runbooks/w7-admin-pages.md` (filed at impl time).

### 3.8 OTel attrs

Per W6 OTel backbone:

| Span name | Attrs |
|---|---|
| `web.handler.runs_list` | `regatta.web.handler=runs_list`, `regatta.web.page_count=<int>`, `regatta.web.sort=<col>`, `regatta.web.status_filter=<val|empty>` |
| `web.handler.run_detail` | `regatta.web.handler=run_detail`, `regatta.web.run_id=<id>`, `regatta.web.work_item_count=<int>`, `regatta.web.truncated=<bool>` |

Tracer wiring uses the existing `internal/orchestrator/otel` package (no new tracer init).

## 4. Existing patterns reused

| Component | Source | Reuse |
|---|---|---|
| `internal/web` handler scaffold | W7 W1 T4, PR #307 | Mux + template loader + CSP middleware. |
| Cookie HMAC `Principal` type | W7 W1 T7 (#303 — about to merge) | Identity surface; W8 swaps the body of `PrincipalFromRequest`. |
| `dbtest.QueryCounter` | W7.0 T2, PR #255 | Asserts ≤2 SQL queries per render in test. |
| Substrate event fold (cursor encoding semantics) | substrate spec §4 | Cursor opaque-bytes encoding; pagination integrity under concurrent writes (R3). |
| W6 OTel attrs | `internal/orchestrator/otel` | Span+attrs naming. |
| `html/template` auto-escape | stdlib | XSS containment on operator-controlled fields (R6). |
| `<time datetime="...">` JS-free local-tz render | HTML5 | Operator-local timezone display without JS (R7). |

## 5. Risk register (R1 - R8)

| ID | Risk | Mitigation |
|---|---|---|
| R1 | Large-DAG OOM on detail page (10k-node run renders + GC pressure) | Cap to 500 nodes per render; overflow → truncate + collapsible "expand" link to chunked sub-route (followup tracking issue). |
| R2 | Query budget blow-out on detail page (N+1 from per-node edge lookup) | Two SQL queries max, asserted by `dbtest.QueryCounter` (W7.0 T2 #255) regression test. CI fails if `>2`. |
| R3 | Cursor pagination integrity under concurrent writes (mid-scroll inserts dupe rows) | LWW via substrate cursor sort-key `(started_at, run_id)`; tuple-strict comparator means concurrent inserts at the boundary are deterministic. |
| R4 | Admin-only enforcement pre-W8 (any authenticated reviewer sees every run) | Single-tenant assumption documented in handler docstring + runbook; W8 retrofits OPA `run.view` policy. Pre-W8 deployment guidance: do not expose `/runs/` over public ingress. |
| R5 | SQL injection on `?before=<cursor>` | Treat as opaque bytes; validate via `substrate.DecodeCursor` (or local equivalent); reject malformed before any SQL touch. Property test ≥1000 random cursor inputs asserts no panic + no SQL pass-through. |
| R6 | XSS on operator-controlled fields (e.g. `prompt_subject`, lane names with `<script>`) | `html/template` auto-escape. `template.HTML` already banned by W1 `lint-web-template-html` Makefile target. Free-text filter `?q=` restricted to `run_id` substring only (run_ids are ULIDs, alphanumeric — no XSS surface). |
| R7 | Timezone display drift (server renders UTC, operator sees their tz, off-by-N-hours bugs) | Pass UTC + render via JS-free `<time datetime="2026-06-01T12:00:00Z">12:00 UTC</time>`. Operator browser converts via stylesheet `:lang(*)` hint; falls back to UTC if no JS. |
| R8 | DAG topology rendering for 1000-node runs (DOM bloat + paint stall) | Paginate detail-page tree to 500 nodes/render + collapsible groups; tree levels >3 collapsed-by-default. |

## 6. Test plan per task

### T8 — `/runs/` admin list

**B-tier (6 named tests, compile + 1 row renders):**

1. `TestRunsList_HappyPath_RendersOneRow` — single seeded run renders + status + lane visible.
2. `TestRunsList_EmptyDB_RendersEmptyBanner` — zero rows → "no runs yet" banner, 200 status.
3. `TestRunsList_CSPHeader_ExactMatch` — re-asserts W1 §3.7 CSP byte-equal.
4. `TestRunsList_Unauthorized_NoPrincipal_Returns401` — missing cookie → 401, no body leak.
5. `TestRunsList_RouteRegisteredOnMux` — handler reachable via the W1 server mux.
6. `TestRunsList_FailSoft_SubstrateMissing` — `runs_view` absent → banner + 200, no 500.

**A-tier (3 tests):**

7. `TestRunsList_CursorRoundtrip` — first page + `?before=<cursor>` → second page disjoint from first.
8. `TestRunsList_StatusFilter` — `?status=failed` → only failed runs in response body.
9. `TestRunsList_QueryBudget_LE2` — `dbtest.QueryCounter` asserts ≤2 SQL queries per render.

### T9 — `/runs/{run_id}` detail page

**B-tier (6 named tests):**

1. `TestRunDetail_HappyPath_RendersTree` — 5-node DAG renders 5 nodes + 4 edges.
2. `TestRunDetail_UnknownRunID_Returns404` — unseeded `run_id` → 404 typed sentinel.
3. `TestRunDetail_CSPHeader_ExactMatch` — re-asserts W1 §3.7 CSP byte-equal.
4. `TestRunDetail_Unauthorized_NoPrincipal_Returns401` — missing cookie → 401.
5. `TestRunDetail_OverflowTruncation` — 600-node DAG renders 500 + "+100 more" banner.
6. `TestRunDetail_TimezoneRenderIsUTCInDataAttr` — `<time datetime=...>` byte-equal UTC.

**A-tier (3 tests):**

7. `TestRunDetail_StateFilter` — `?state=failed` → only failed work-items.
8. `TestRunDetail_QueryBudget_LE2` — `dbtest.QueryCounter` asserts ≤2 SQL queries.
9. `TestRunDetail_CycleProtection_TerminatesWithBanner` — synthetic cyclic edge → render terminates + "cycle truncated" indicator (not stack overflow, not OOM).

**A+ tier (1 property test):**

10. `TestRunDetail_CursorRoundtrip_Property` — ≥1000 random `(started_at, run_id)` tuples → `EncodeCursor → DecodeCursor` byte-equal; malformed inputs reject without panic.

## 7. Grade rubric verbatim

### B — floor (ships)

- [ ] T8 handler + `runs_list.tmpl` mounted on W1 mux; 6 B-tier tests pass.
- [ ] T9 handler + `run_detail.tmpl` + `_workitem_node.tmpl` mounted; 6 B-tier tests pass.
- [ ] CSP header byte-equal W1 §3.7 on both routes.
- [ ] `make check` clean after both T8 + T9 land.
- [ ] No new third-party deps in `go.mod`; htmx + Tailwind unchanged from W1.

### A — target (expected)

All B, plus:

- [ ] T8 cursor pagination roundtrip green (test #7).
- [ ] T8 status filter green (test #8).
- [ ] T9 state filter green (test #7).
- [ ] T9 query budget ≤2 SQL asserted via `dbtest.QueryCounter` (test #8).
- [ ] RBAC stub: `Authorizer.Check` call site present (guarded by `h.authz != nil`), W8-ready.
- [ ] Tracking issues filed for all §10 followup items + cited by number in PR body (per `feedback_unaddressed_load_bearing`).

### A+ — stretch (aspirational)

All A, plus:

- [ ] T9 query budget asserted ≤2 SQL queries (already in A — promote here only if mutation-coverage ≥95%).
- [ ] Cursor roundtrip property test ≥1000 cases green (test #10).
- [ ] Render-time ≤50 ms p95 on 100-row fixture (`make bench-ui` extended).
- [ ] Lighthouse / axe-core accessibility score ≥95 on both pages.
- [ ] Zero magic numbers in T8 / T9 handlers — all caps named in `const.go` (per W1 A+).

## 8. File-disjoint impl preview

| Task | Files touched | Disjoint? |
|---|---|---|
| T8 | `internal/web/runs_list.go`, `runs_list_test.go`, `templates/runs_list.tmpl` | ✓ |
| T9 | `internal/web/run_detail.go`, `run_detail_test.go`, `templates/run_detail.tmpl`, `templates/_workitem_node.tmpl` | ✓ (disjoint from T8) |
| T10 (follow-on; W1 §7) | `internal/web/cursor.go`, `cursor_test.go`, plus `tests/e2e/playwright/runs.spec.ts` | shares mux with T8 + T9 but file-disjoint |

Owner per `feedback_shared_primitive_owner`: cursor primitive owner = T10 implementer. T8 ships an inline cursor (private surface) that T10 swaps to the shared `internal/web/cursor.go`. The swap is a pure refactor; no behavior change.

## 9. Sequencing

W7.2 (this spec) lands AFTER W7 W1 closes:

- W1 T4 (server scaffold, PR #307) — merged.
- W1 T5 (Tailwind vendor) — in flight.
- W1 T6 (approval flow) — in flight.
- W1 T7 (`Principal` cookie HMAC, PR #303) — about to merge.

W7.2 is independent of W8 (uses W7 W1 `Principal` directly with `h.authz=nil` until W8 ships). W7.2 lands BEFORE W9 — W9 T3 wires the "replay" button into `run_detail.tmpl`. W7.2 leaves a stable surface for that.

Dependency chain summary:

```
W7 W1 (T4 + T7) ──▶ W7.2 T8 + T9 ──▶ W7.2 T10 (Playwright + cursor primitive) ──▶ W9 T3 (replay button)
                                  ──▶ W8 (Authorizer.Check retrofit)
```

## 10. Deferred (tracking issues to file at impl time)

Per `feedback_unaddressed_load_bearing` — file before merge, cite by number in PR body.

- [ ] **Wave 3 T12 — DAG graphviz / SVG visualization** (R8; W1 §7 W7.4 followup also references).
- [ ] **Substrate-fact subscription for live updates via W11 blackboard** (replaces 5s htmx poll; SSE / WebSocket upgrade path).
- [ ] **Cross-tenant admin index** (depends on W8 OPA `run.view` policy + tenant scope).
- [ ] **Edit / rerun / replay buttons on detail page** (W9 replay-diff harness scope).
- [ ] **Detail-page tree pagination beyond 500 nodes** (R1 + R8 chunked sub-route — `GET /runs/{run_id}/items?after=<cursor>`).
- [ ] **`runs_view` materialized vs fold-on-read** (W1 §3.10 reconciliation; resolve once substrate Phase 3 ships).
- [ ] **Detail-page tree-search filter (substring across lane + state)** (deferred; not in v1 risk register).
- [ ] **Per-user saved filter sets** (W8 OPA + UX backlog — explicitly out of v1).

Each annotated `→ file as gh issue, cite as "Tracking: #NNN" in PR body before merge`.

## 11. References

- W7 W1 spec: `2026-06-01-w7-operator-web-ui-design.md` (this directory).
- Unified substrate spec: `2026-06-01-unified-substrate-design.md` §2.1 + §4.
- W6 OTel backbone: `2026-05-31-mvp-3-w6-otel-backbone.md`.
- W8 OPA RBAC: `2026-06-01-w8-opa-rbac-design.md`.
- W9 replay harness: `2026-06-01-w9-replay-diff-harness-design.md`.
- W7.0 T2 query counter: PR #255.
- W7 W1 T4 handler scaffold: PR #307.
- W7 W1 T7 cookie HMAC `Principal`: PR #303.
- Memory: `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_unaddressed_load_bearing`, `feedback_shared_primitive_owner`, `feedback_verify_before_asking`.

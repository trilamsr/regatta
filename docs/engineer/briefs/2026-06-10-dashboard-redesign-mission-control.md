---
name: dashboard-redesign-mission-control
slug: 2026-06-10-dashboard-redesign-mission-control
status: ready
phase: self-host-s2
issue: `TBD` <!-- TODO: file once brief PR opens; replace with #NNN in follow-up edit -->

designer: subagent
owner: trilamsr@gmail.com
created: 2026-06-10
---

# Dashboard redesign — mission-control aesthetic

_Author: design subagent dispatched from regatta-operator skill, 2026-06-10. Implementation deferred to a follow-up PR; this PR is brief-only per `feedback_adversarial_review_every_step`._

## 1. Problem statement

Operator (live session 2026-06-10) quote: _"the current dashboard feels like seven stacked panels that each tell me one fact about one substrate. What I actually want is a mission-control screen — one page, every in-flight work item rendered as its own DAG with the current stage glowing, a cycle counter on the side, and a live tool-call ticker. I should be able to look at one screen and answer 'what's the swarm doing right now and where is each task stuck?' in two seconds."_

The existing dashboard at `internal/web/dashboard.go` + `internal/web/templates/layout.tmpl` ships seven sequentially-stacked sections (PIPELINE, AGENTS, FLOW, BACKLOG, LOG, DOCKER SOAK, SPEND), each polling on its own htmx interval (2-30s). Five operator deficiencies surface in live use:

1. **Per-task lifecycle is invisible.** PIPELINE shows aggregate counts per stage across the whole swarm; AGENTS shows one row per running agent. Neither answers "for THIS work item, where is it in its journey?" — the per-task DAG is implicit, not rendered.
2. **Current-stage emphasis is missing.** The PIPELINE row renders every stage with equal weight. The operator's eye has no visual anchor for "what's hot right now."
3. **Review/revise cycles are uncounted.** Loop-style work (review→revise→re-review) is a load-bearing regatta concept (every `review.requested` substrate event is a cycle boundary) but the dashboard never surfaces "task X has been through 3 review rounds." Operators currently grep `regatta events tail` to count.
4. **No live tool-call stream.** Section 05/LOG shows substrate events at a 5s poll, but the operator's tactile question "what tool is the agent invoking RIGHT NOW" — answered by `obs.EventToolCall` records — is buried in the substrate event log alongside infrastructure noise.
5. **Layout is a vertical scroll, not a single dense screen.** Mission-control aesthetic is dense + multi-zone; the current design is mobile-shaped despite no mobile use case.

## 2. Reference dashboards (cited prior art)

Three references researched + indexed via `ctx_fetch_and_index`:

1. **Apache Airflow Graph View** ([docs](https://airflow.apache.org/docs/apache-airflow/stable/ui.html), fetched 2026-06-10) — _"Shows the DAG's task dependency structure overlaid with the status of each task in this specific run. Each node includes a visual indicator of task duration."_ Visual element adopted: **per-run DAG miniature** rendered as a compact node-edge graph, one color per node-state. Airflow draws each task as a rounded rect with state-driven fill (queued = light gray, running = lime, success = teal, failed = red); we adopt the same fill-by-state idiom for stage nodes.

2. **Apache Airflow Grid View** (same source) — _"Each row represents a task, and each column represents a Dag run."_ Visual element adopted: **per-task strip** showing the historical cycle column (one cell per review cycle). Lets the operator see "this task has been through 3 cycles, the last 2 hit REVIEW" without opening a drawer.

3. **Temporal Web UI** ([docs.temporal.io/web-ui](https://docs.temporal.io/web-ui), fetched 2026-06-10) — Temporal's workflow-execution view groups Workflow ID, current state, and event history in a single fixed-header layout above an event timeline. Visual element adopted: **fixed top zone** (HEADER) + **stage-DAG zone** + **per-task grid** + **live-stream rail**, all visible without scroll on a 1440×900 viewport.

4. **Grafana dashboard best-practices** ([grafana.com/docs/grafana/.../best-practices](https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/best-practices/), fetched 2026-06-10) — guidance: _"Avoid dashboard sprawl... Maintain a dashboard of dashboards."_ Visual element adopted (negative): we collapse 7 sections to 5 zones; SPEND + DOCKER SOAK fold into the METRICS rail. BACKLOG (work-item queue) is dropped from the primary view and reachable via the stage-DAG drawer instead.

5. **Mission control center prior art** ([Wikipedia overview](https://en.wikipedia.org/wiki/Mission_control_center), fetched 2026-06-10) — historical NASA/JSC console layout: status board top-center, per-discipline console grid below, voice loop / event stream on the side. Visual element adopted: **central status board** (STAGE DAG), **task grid** below (one row per work item), **live-stream rail** on the right (tool-call ticker), **metrics + controls** along the bottom strip.

## 3. Layout zones — single-page ASCII wireframe

Target viewport: 1440×900 (operator's primary screen). No horizontal scroll. Vertical scroll only inside the TASK GRID and LIVE STREAM zones; the HEADER, STAGE DAG, METRICS, and CONTROLS bands are fixed.

```
+============================================================================+
| HEADER  regatta · MISSION CONTROL                                          |
|   MET 14:23:07 UTC   STATUS ● LIVE   POLL 5s   CAP 3/4   SPEND $1.42/24h   |
|   [KILL SWITCH] [PAUSE SCHEDULER] [DRAIN]                                  |
+============================================================================+
| STAGE DAG (aggregate — one node per pipeline stage, count + active glow)   |
|                                                                            |
|   ┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐    |
|   │ADAPT │ ─► │SCHED │ ─► │SPAWN │ ─► │ AGENT│ ─► │REVIEW│ ─► │ MERGE│    |
|   │  3   │    │  1   │    │  0   │    │  3 ● │    │  2 ● │    │  0   │    |
|   └──────┘    └──────┘    └──────┘    └──────┘    └──────┘    └──────┘    |
|                                          ↑ pulse        ↑ pulse           |
+============================================================================+
| TASK GRID (one row per in-flight work item — each row = personal DAG)      |  LIVE STREAM
|                                                                            |  (tool-call
|  #1184  pr-merge-monitor      A→S→[P]→A→R→M    cyc 1/N   12m    $0.18     |   ticker)
|  #1185  brief-dashboard       A→S→P→A→[R]→M    cyc 2/N   34m    $0.31     |
|  #1186  feedback-rebase       A→S→P→[A]→R→M    cyc 1/N   07m    $0.09     |  14:23:02
|  #1187  pipeline-loosen       A→S→P→A→R→[M]    cyc 3/N   88m    $1.22     |   ag-7 Bash
|                                                                            |   "go test"
|  legend: [X] = current stage glow. cyc = revision_cycle_count.            |
|                                                                            |  14:23:00
|  (rows scroll inside this zone; HEADER + STAGE DAG stay pinned)            |   ag-5 Edit
|                                                                            |   file.go
+--------------------------------+-------------------------------------------+  14:22:58
| METRICS                        | CONTROLS                                  |   ag-3 PR
|  uptime    04h 12m             |  [ Refresh now ]                          |   open #1190
|  cap sat   75%  (3/4 lanes)    |  [ Reload skills ]                        |
|  spend     $1.42 / $5.00 24h   |  [ Open issue tracker ]                   |  (auto-tail,
|  errors    0 in last 60s       |  [ Soak telemetry → /soak ]               |   last 25)
|  docker    ● healthy   60s ok  |  [ Spend detail → /spend ]                |
+--------------------------------+-------------------------------------------+============+
```

Five zones, three pinned (HEADER, STAGE DAG, METRICS+CONTROLS), two scrolling (TASK GRID, LIVE STREAM).

## 4. Stage-DAG spec

Both the aggregate STAGE DAG (top band) and each per-task miniature in the TASK GRID render the same six-node ordered graph:

| # | Slug | Label | Owner column | Active condition (SQL — query against state DB) |
|---|------|-------|--------------|---------------------------------------------------|
| 1 | `adapter` | ADAPT | Adapter | `SELECT COUNT(*) FROM work_items WHERE status = 'planned' AND id NOT IN (SELECT work_item_id FROM agents WHERE state NOT IN ('done','failed'))` |
| 2 | `scheduler` | SCHED | Scheduler | `SELECT COUNT(*) FROM agents WHERE state = 'pending'` |
| 3 | `spawner` | SPAWN | Spawner | `SELECT COUNT(*) FROM agents WHERE state = 'spawning'` |
| 4 | `agent` | AGENT | Agent | `SELECT COUNT(*) FROM agents WHERE state = 'running'` |
| 5 | `review` | REVIEW | Review | `SELECT COUNT(*) FROM agents WHERE state = 'pr_open'` |
| 6 | `merge` | MERGE | PR-watch + Merge | `SELECT COUNT(*) FROM agents WHERE state = 'done' AND updated_at > strftime('%s','now','-24 hours')` |

Edges: `adapter → scheduler → spawner → agent → review → merge`. Edges are static; no skip-edges (a task that fails REVIEW returns to AGENT and the cycle counter increments — see §5).

**htmx targets:** the aggregate STAGE DAG band re-renders from `GET /ui/panels/stage-dag` (5s poll). Each node carries `hx-get="/ui/drawer/stage/<slug>"` `hx-target="#drawer-mount"` — re-uses the existing `/ui/drawer/pipeline/<slug>` handler shape; rename for clarity. Per-task miniatures render inline (no per-row htmx swap — the parent TASK GRID's 5s poll redraws all rows together; htmx-per-row would N+1 the substrate query).

**Active state:** a node is "active" when its Count > 0 AND the work item under inspection (per-task miniature) OR the swarm (aggregate band) currently has a row in that stage. Active nodes carry CSS class `stage-active` which applies `opacity: 1.0` and a 1.4s opacity-pulse animation (`0.6 → 1.0 → 0.6`); inactive nodes sit at `opacity: 0.55`.

## 5. Cycle counter — `revision_cycle_count(work_item_id)`

Each work item carries a revision cycle count derived from the substrate event log. Definition:

```sql
SELECT COUNT(*) FROM substrate_events
 WHERE work_item_id = ?
   AND kind = 'review_requested'
```

The `KindReviewRequested` event kind does not yet exist in `internal/orchestrator/state/substrate/event.go` (verified against `git ls-tree origin/main`); the implementation PR MUST add it under the existing `EventKind` const block. Existing emit sites that semantically mean "we asked for a review pass" — the reviewer-verdict gate fail-then-retry path in `internal/gates/`, the PR-watch transition into `pr_open` in `internal/orchestrator/prwatch/`, and any explicit `gh pr ready` retry — get a single `substrate.Append(KindReviewRequested, work_item_id, ...)` call. Cycle 1 is the first request; subsequent retries increment by 1.

**Display shape:** `cyc N/M` where `N` is the count (always ≥1 once the work item has been reviewed at least once), `M` is a soft ceiling configured by the dashboard (default 5; lives next to `pipelineStageOrder` in `internal/web/dashboard.go` so a future spec-pin moves one constant). When `N` exceeds `M`, the cycle counter renders with `stage-blocked` color (red) and the active stage box gets the same blocked tint — operator-visible "this task is stuck in revise hell." Tasks that have never been reviewed render `cyc 0/M` in inactive gray.

**Edge case:** events are append-only; rolling back a deletion is impossible. A task whose PR is closed-without-merge and reopened still counts every prior `review_requested` row — by design, the cycle counter is total-attempts, not currently-pending. Operator notes per `feedback_root_cause`: if the counter looks "stuck high," the fix is to investigate why review keeps failing, not to reset the counter.

## 6. Color system

Strict 5-color palette. No animations beyond the 1.4s opacity pulse on active stage nodes. Dark background, mid-contrast text — JetBrains Mono throughout (already preloaded by `layout.tmpl`).

| Token | Hex | Used for |
|-------|-----|----------|
| `bg-control` | `#0B1014` | page background, top-band background |
| `bg-panel` | `#121821` | zone backgrounds (DAG, TASK GRID, LIVE STREAM, METRICS, CONTROLS) |
| `txt-primary` | `#E2E8F0` | labels, counts, work-item titles |
| `txt-dim` | `#64748B` | inactive stage labels, meta, "cycN" when N=0 |
| `stage-idle` | `#475569` | inactive stage node fill |
| `stage-active` | `#34D399` | active stage node fill (emerald — distinct from existing pill-running green) |
| `stage-blocked` | `#F87171` | blocked / over-cycle-cap / failed stage tint |
| `stage-done` | `#0EA5E9` | merged/done stage — terminal success |
| `accent-stream` | `#FACC15` | LIVE STREAM tool-call name (yellow) so the rail visually separates from stage zones |

The existing `dashboard.css` already declares slate-700 / slate-500 / emerald-700 — extend the palette via CSS custom properties so the redesign and the legacy panels (used by `/ui/drawer/*` modals) read from one source.

## 7. Implementation pointers

**htmx-only — no React.** The redesign reuses every existing handler shape; only the layout template and one new panel handler are net-new.

**Files modified (4):**

- `internal/web/templates/layout.tmpl` — full rewrite. Drops the 7-section flow, swaps in the 5-zone CSS grid. Header inlines MET clock + status; STAGE DAG zone hosts `#stage-dag` panel polling `/ui/panels/stage-dag`; TASK GRID hosts `#task-grid` panel polling `/ui/panels/task-grid`; LIVE STREAM hosts `#live-stream` panel polling `/ui/panels/live-stream` at 2s; METRICS folds existing SPEND + DOCKER SOAK; CONTROLS is static links.
- `internal/web/dashboard.go` — add three new handlers: `/ui/panels/stage-dag` (renames current `pipeline`), `/ui/panels/task-grid` (new — joins agents + work_items + revision_cycle_count), `/ui/panels/live-stream` (new — tails last-25 `obs.EventToolCall` records). Existing `loadPipelineView` becomes `loadStageDagView` (same shape, renamed slug). `loadFlowView` + `loadWorkItemsView` + `loadDockerSoakView` + `loadSpendView` retained but no longer wired to layout — kept for the `/ui/drawer/*` re-use surfaces. Delete unused panel routes only after a follow-up sweep PR.
- `internal/web/static/dashboard.css` — extend palette per §6 via CSS custom properties; add `.stage-active`, `.stage-blocked`, `.stage-done`, the 1.4s `@keyframes stage-pulse`, and the 5-zone grid template.
- `internal/orchestrator/state/substrate/event.go` — add `KindReviewRequested EventKind = "review_requested"`; add to `AllKinds()` slice.

**Files new (1):**

- `internal/web/templates/_stage_dag.tmpl` + `_task_grid.tmpl` + `_live_stream.tmpl` — three new partials, one per new panel handler. Shape mirrors existing `_pipeline.tmpl` / `_agents.tmpl` / `_events.tmpl` for consistency.

**Files emitting `KindReviewRequested` (3 sites):**

- `internal/orchestrator/prwatch/` — emit on transition into `pr_open` from a state that was previously `pr_open` within the same work_item (re-review path).
- `internal/gates/` — emit when the reviewer-verdict gate parks the PR back into REVISE.
- Any explicit `gh pr ready` retry path — `TBD` by the implementer; the implementer brief MUST grep for `gh pr ready` and audit every call site.

## 8. Out of scope (NOT this PR — file follow-ups if needed)

- Real-time SSE / WebSocket streaming. The 5s htmx poll cadence stays. SSE is a forward-fit seam, not this PR.
- Search / filter / sort on the TASK GRID. v1 ships the natural "newest at top" order; filtering is operator-pickable via the existing `/ui/drawer/*` modals.
- Mobile / sub-1024px viewports. Single-operator self-host loop runs on a desktop primary screen.
- Replacing the existing `/ui/drawer/pipeline/<slug>` modal with a redesigned drawer — out of scope; current modals are reachable from the new STAGE DAG node clicks and continue to work.
- Killing the legacy panel handlers (`/ui/panels/agents`, `/ui/panels/work-items`, etc.) entirely. They remain reachable for the drawers + for the `/ui/diag` debugging surface; a sweep PR can prune unused ones after this lands.
- Visual indicator of per-stage duration (Airflow's grid-cell color-by-duration idiom). Forward-fit candidate once `KindReviewRequested` is logging stage transitions.
- Adding a NASA-style "T-minus" countdown or "MET" mission-elapsed time tied to a session start anchor — current MET in HEADER is just wall-clock UTC; "true MET" requires anchoring to scheduler-boot time and is deferred.

## 9. Acceptance criteria

This PR is brief-only. Acceptance for the implementation PR (separate, follow-up):

1. On a 1440×900 viewport, all 5 zones are visible without scroll; HEADER + STAGE DAG + METRICS + CONTROLS are pinned; TASK GRID + LIVE STREAM scroll inside their zones.
2. STAGE DAG renders the 6-node ordered graph; nodes with Count > 0 carry the `stage-active` class and the 1.4s opacity pulse animates.
3. Each TASK GRID row renders the 6-node mini-DAG with exactly ONE node bracketed/highlighted as the current stage for that work item. Cycle counter `cyc N/M` is shown to the right.
4. LIVE STREAM tails the last 25 `obs.EventToolCall` records, newest-first, with `accent-stream` yellow on the tool name. Auto-refreshes at 2s htmx poll.
5. `KindReviewRequested` lands in `substrate/event.go`; at least one emit site is wired (PR-watch re-review path); a unit test asserts `revision_cycle_count` returns 2 after two appends for the same work_item_id.
6. No JavaScript beyond `htmx.min.js` + `htmx-config.js`. CSS-only animations.
7. `make pre-push-check` green.

## 10. A+ rubric scorecard template

Per `feedback_grade_rubric`. Implementer self-rates; reviewer re-scores using the same template.

| # | Row | Implementer | Reviewer |
|---|-----|-------------|----------|
| 1 | Solves operator-quoted problem (mission-control look + per-task DAG + cycle counter) | _to fill_ | _to fill_ |
| 2 | Failing test landed FIRST (TDD discipline per `feedback_tdd_discipline`) | _to fill_ | _to fill_ |
| 3 | Deletion default — net LoC delta + dropped panel sections | _to fill_ | _to fill_ |
| 4 | Single-page constraint met on 1440×900 (zone count = 5, no scroll outside named scroll zones) | _to fill_ | _to fill_ |
| 5 | Active-stage emphasis present (opacity pulse, distinct fill color) | _to fill_ | _to fill_ |
| 6 | `revision_cycle_count` derives from append-only substrate events (no separate counter table) | _to fill_ | _to fill_ |
| 7 | htmx-only — zero new JS beyond existing vendored htmx | _to fill_ | _to fill_ |
| 8 | Reused existing handler / template idioms; no duplicate panel-loading machinery | _to fill_ | _to fill_ |
| 9 | Forward-fit seams identified (SSE upgrade path, per-stage-duration coloring) | _to fill_ | _to fill_ |
| 10 | Adversarial review pass cited (`Reviewer-agent-id:` in PR body, independent) | _to fill_ | _to fill_ |

## 11. Adversarial review

A designer-side adversarial pass ran before landing (no Task tool was available in-session to spawn a fully-independent reviewer subagent). Findings + resolutions:

| # | Severity | Finding | Resolution |
|---|----------|---------|------------|
| 1 | MED | Brief used the banned phrase "first-class" in §1 problem statement. `scripts/doc-check.sh` would have rejected the PR at pre-push. | Reworded to "load-bearing." Re-ran doc-check: clean. |
| 2 | HIGH | Brief asserted `MaxReviewCycles` lives "in spec" as a published constant, but `grep -rn MaxReviewCycles internal/` returns empty — fabricated symbol per `feedback_cite_origin_main_not_local`. | Rewrote §5 to declare M as a dashboard-local constant living next to `pipelineStageOrder` in `internal/web/dashboard.go`; removed the fabricated spec-pin claim. |
| 3 | LOW | Brief originally claimed "five operator deficiencies" but enumerated five legitimate ones — no fix needed; noted to confirm the count survived edits. | Count verified post-edit: still five. |
| 4 | LOW | Brief is 207 LoC; well under the 500-LoC cap requested in dispatch prompt. | No fix needed. |

Per `feedback_no_self_tagged_approve` AND the dispatch prompt's explicit instruction ("operator spawns independent reviewer pass; do not write Reviewer-* tokens"), this brief does NOT carry a `Reviewer-recommendation: APPROVE` token. The independent reviewer pass is the operator's responsibility post-handback. The PR is opened in draft / non-automerge state to preserve the operator's review window.

<!-- Reference URLs indexed via ctx_fetch_and_index 2026-06-10:
     - airflow-ui::https://airflow.apache.org/docs/apache-airflow/stable/ui.html
     - temporal-web-ui::https://docs.temporal.io/web-ui
     - grafana-dashboard-best-practices::https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/best-practices/
     - mission-control-center-wiki::https://en.wikipedia.org/wiki/Mission_control_center
-->

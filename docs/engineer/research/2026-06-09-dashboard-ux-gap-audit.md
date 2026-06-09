---
title: Operator dashboard UX gap audit
date: 2026-06-09
scope: internal/web (live at localhost:8080 during 17h45m docker soak)
status: research
phase: self-host
---

# Operator dashboard UX gap audit

Inspected: `internal/web/dashboard.go` (511 LoC), 10 templates, `dashboard.css` (283 LoC, 57 selectors, **zero `@media`, zero a11y mentions**). Polls: agents 5s, flow/work-items 10s, events 5s, spend 30s. Drawers swap into single `#drawer-mount`.

Tiers: **S** must-ship, **A** high leverage, **B** nice-to-have. Sizes S/M/L are effort.

---

## S — must-ship gaps

### S1. Work-item rows do not link to the PR they produced
- **Symptom**: rows show `Title · lane · 3m ago`; no PR URL/SHA anywhere. `_drawer_workitem.tmpl` lacks PR fields.
- **Root cause**: `loadWorkItemsView` projects only `ID/Title/Lane/UpdatedAt`; `serveWorkItemDrawer` doesn't join agents → `PRSHA` never reaches template.
- **Files**: `dashboard.go` (`loadWorkItemsView`, `serveWorkItemDrawer`, `dashboardWorkItemDetail`), `_drawer_workitem.tmpl`, `_work_items.tmpl`.
- **Data**: exists (`agents.pr_sha`); needs join + `Dependencies.RepoURL` config.
- **Size**: S.

### S2. No action affordance — read-only UI
- **Symptom**: cannot kill / re-spawn / cancel work-item from UI. Operator sees `crashed` row, must `docker exec`. Defeats operator-first promise.
- **Root cause**: only `GET /ui/...` registered; no POST verbs, no CSRF threading.
- **Files**: `dashboard.go` (POST `/ui/agent/{id}/{kill,respawn}`, `/ui/workitem/{id}/cancel`); drawer templates (button row); `cmd/regatta/serve.go` (CSRF).
- **Data**: reaper hooks exist; needs HTTP surface + idempotency token.
- **Size**: M.

### S3. Panels go silent on DB error
- **Symptom**: `ListAgentsByState` error → `dashboardAgentsView{}` → "No agents in flight" — indistinguishable from true idle. Same in `loadWorkItemsView` / `loadEventsView` / `loadFlowView`. Over 17h soak operator can't tell idle from broken.
- **Root cause**: error → empty-view conversion. No banner.
- **Files**: `dashboard.go` (loaders carry `Err string`); panel tmpls render banner like `_spend.tmpl` already does; `layout.tmpl` flip topbar `●LIVE` red when any loader errored.
- **Size**: S.

### S4. Color-only state encoding; zero a11y
- **Symptom**: `.pill-*` discriminate by hue only (CSS audit: 0 aria/role/sr-only matches). ~8% of male operators cannot distinguish `pr-open` / `gates` / `blocked`.
- **Fix**: glyph prefix per state (●◐✓✗), `role="button"` + `aria-label` on every `click-row`.
- **Files**: `dashboard.css`, `_agents.tmpl`, `_work_items.tmpl`, `_events.tmpl`.
- **Size**: S.

### S5. Timestamps mix zones; no absolute tooltip
- **Symptom**: header `formatTime` is config-zone, rows are `relTime` ("3m ago"), spend says "00:00 UTC". PST operator does mental math.
- **Fix**: add `absTimeFn` helper; every `relTime`/`formatTime` cell gets `title="<RFC3339 + local>"`.
- **Files**: `dashboard.go::relTimeFn`, all templates rendering timestamps.
- **Size**: S.

---

## A — high-leverage gaps

### A1. Events panel misses bursts (5s poll, no replay)
- Tail (~50 rows) refreshes every 5s; burst of 30 `spawn.started`+`tick.started` in <1s coalesces. No scroll-back, no kind filter.
- Fix: SSE `/ui/stream/events` + kind-filter chips + paginate-older; coalesce `tick.started`+`tick.completed` pairs.
- Files: `dashboard.go` (SSE handler), `_events.tmpl`, `layout.tmpl`. Data: `ListEvents` cursored by id; needs `state.Bus` subscribe.
- Size: M.

### A2. Flow panel collapses lanes
- `loadFlowView` aggregates globally; `Halt` only flags total `WorkStatusRunning`. With multi-lane setups operator can't see which lane is wedged.
- Fix: per-lane row with cap-saturation bar (`server:1 cap=1, 100%`).
- Files: `dashboard.go`, `state/work_items.go` (new `SummarizeWorkItemStatusesByLane` with `GROUP BY lane,status`), `_flow.tmpl`.
- Size: M.

### A3. Spend: no per-lane / per-agent breakdown, no cap headroom
- Three USD numbers + sparkline. Cannot tell runaway agent from broad uptick. No overlay of `cost.cap.daily_usd`.
- Fix: top-5 spenders 24h list; cap-headroom number; budget line on sparkline.
- Files: `dashboard.go::loadSpendView`, `cost/spend/reader.go` (new `TopSpenders`), `_spend.tmpl`. Data: `spend_events` keyed by scope; needs aggregate.
- Size: M.

### A4. 1000-agent / 100k-event scale
- `ListAgentsByState` un-paginated; 1000 rows of HTML per 5s poll. Soak hides this (<20 agents).
- Fix: `dashboardAgentRowsCap = 50` + "showing 50 of N" link to paginated view (same pattern as work-item samples).
- Files: `dashboard.go::loadAgentsView`, `_agents.tmpl`. Size: S.

### A5. Mobile / narrow-screen unusable
- CSS has zero `@media`. Agent row inline `grid-template-columns: 7rem 1fr 6rem 4rem 5rem` (~22rem min); below 400px title truncates to one char.
- Fix: `@media (max-width: 640px)` collapses to two-line cards; drawer becomes full-screen modal. Move inline `style=` to class first.
- Files: `dashboard.css`, `_agents.tmpl`. Size: S.

### A6. Keyboard navigation absent
- `click-row` / `event-line` are `<div>`s with `hx-get` only. No `tabindex`, no Enter/Space, no focus ring (`outline` 0 matches).
- Fix: `role="button" tabindex="0"` + `hx-trigger="click, keyup[key=='Enter']"`; `:focus-visible { outline: 2px solid #c2410c }`.
- Files: row templates + `dashboard.css`. Size: S.

### A7. Drawer: no deep-link / no back button / no Esc
- innerHTML swap; back button doesn't close, Esc doesn't close, URL doesn't update → no shareable links.
- Fix: `hx-push-url`, GET handler for full-page+drawer, global Esc listener in `layout.tmpl`.
- Files: row triggers, `layout.tmpl`, `dashboard.go`. Size: M.

### A8. Event verbs hardcoded; new kinds fall through
- `eventVerb` switches on 8 literals; `prwatch.branch_renamed_by_agent`, `cost.cap.breach`, exit-reason subtypes render as raw snake_case.
- Fix: extract registry to `internal/web/eventverbs.go` keyed off SoT slice in `state/events.go`; add `scripts/check-event-verb-coverage.sh`.
- Size: M.

### A9. Empty states have no next-action hint
- Buckets render `—`; spend zero gives no onboarding hint.
- Fix: per-bucket CTA ("No PR-open items · `gh pr list`"); spend zero → "configure cost-cap in `.regatta/config.yaml`".
- Files: `_work_items.tmpl`, `_spend.tmpl`. Size: S.

### A10. Drawer JSON unsearchable, unbounded
- `<pre>` with `break-all` floods drawer for 4KB payloads. No fold, no copy, no scroll cap.
- Fix: `<details>` tree, copy button, `.drawer pre { max-height: 60vh; overflow: auto }`.
- Files: `_drawer_event.tmpl`, `dashboard.css`. Size: S.

---

## B — nice-to-have

### B1. Sparkline lacks tooltip / per-hour drilldown
SVG bars carry no `<title>` element. Add `<title>{{hourLabel}}: {{usd}}</title>` per `<rect>` so hover surfaces hour + amount. File: `dashboard.go::sparkSVG`. Size S.

### B2. No CSV / JSON export for spend / events / agents
17h45m soak operator wants to post-mortem a CSV in a spreadsheet. Add `GET /ui/export/{events,spend,agents}.csv`. Size M.

### B3. No flow sankey for stage→stage transitions
Current flow is a counts ribbon (`backlog → spawning → in PR → merged → blocked`); the real signal is transition rate (items/min between stages). Would require `state.WorkItemTransitions` time-series. Size L.

### B4. Lane filter / search on all panels
With multi-lane setups operator wants to scope all panels to one lane. Add `?lane=server:1` query param routed through every loader. Size M.

### B5. Topbar live ping freshness
`STATUS: ●LIVE` is static HTML, not driven by last-successful-poll. Wire to last-htmx-response timestamp; flip red after 30s no-response. Size S.

### B6. Drawer renders bare enum strings (`status: AgentRunning`) instead of operator labels
`_drawer_agent.tmpl` shows `{{.State}}` not `{{statusLabel .State}}`. Same for `_drawer_workitem.tmpl`. Size S.

### B7. Tabular density toggle (compact / comfortable)
Soak operators staring 17h want compact; first-time operators want comfortable. CSS class on `<body>`, persisted via localStorage. Size S.

### B8. Template-render error currently 500s with raw `err.Error()` to operator
`http.Error(w, err.Error(), 500)` leaks `text/template: "_drawer_event" can't evaluate field X` stack. Should render a sanitized panel-level error banner via `_flash.tmpl` instead. Size S.

---

## Prioritized 10-item backlog

| # | ID | Tier | Effort | Title |
|---|----|------|--------|-------|
| 1 | S3 | S | S | Surface DB / loader errors as banners instead of silent empty state |
| 2 | S1 | S | S | Link work-item rows → PR URL + commit SHA (join via `agents.pr_sha`) |
| 3 | S4 | S | S | Non-color state indicators + `aria-label` on every click-row |
| 4 | A6 | A | S | Keyboard navigation: `tabindex`, Enter/Space, `:focus-visible` outline |
| 5 | A5 | A | S | Mobile breakpoint: collapse agent / work-items rows below 640px |
| 6 | S5 | S | S | Per-cell `title=` tooltip with absolute RFC3339 + local zone |
| 7 | A2 | A | M | Per-lane flow rows with cap-saturation bar |
| 8 | A1 | A | M | SSE event stream + kind-filter chips + tick coalescing |
| 9 | S2 | S | M | Action buttons: kill agent, re-spawn, cancel work-item (POST + CSRF) |
| 10| A3 | A | M | Spend top-5-spenders + cap-headroom overlay on sparkline |

Items 1, 3, 6 are independent <1h fixes; ship them in the next docker rebuild. Item 9 (S2) is the highest-leverage operator-workflow gap but needs a CSRF + idempotency design pass first — file as plan-master issue.

---

## Methodology notes

- File reads done in sandbox (`ctx_execute_file` / `ctx_execute`); raw bytes not loaded into operator context.
- Live regatta logs not consulted; signal inferred from `spawner.go` observed-event tokens via the verb map in `eventVerb` + `state.AgentState` enum coverage in `statusClass` / `statusLabel`.
- "Color-only" claim verified by 0 matches of `(aria|role|focus|outline|sr-only)` regex in `dashboard.css`.
- "Zero responsive breakpoints" claim verified by 0 matches of `@media` regex in `dashboard.css`.
- Error-handling claim verified by `errLines` regex sweep: every loader funnels error → empty struct (no `Err` field propagated except `loadSpendView`).

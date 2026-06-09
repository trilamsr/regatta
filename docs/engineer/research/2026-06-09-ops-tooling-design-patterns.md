---
title: Operator-tool design patterns for the regatta dashboard
date: 2026-06-09
status: research
audience: web/dashboard implementers, design subagents
scope: prior-art survey of 8 operator UIs; concrete adoption shortlist
---

# Operator-tool design patterns for the regatta dashboard

Regatta's operator runs the binary on their own machine, watching it dispatch ~50 PRs/day. The dashboard (`internal/web/`) already commits to a maritime-instrumentation idiom: aged-paper background, signal-orange accents, JetBrains Mono, htmx + Tailwind. Shipped surfaces: agents table, work-item buckets, event stream, flow viz, spend spark, drawer drill-down. The gap is operator-throughput — decisions per minute before the operator must drop to a terminal or GitHub.

## 1. Bloomberg Terminal — dense data, keyboard-driven, single-window operator workflow

- **4-pane tiled workspace, no overlays.** Every context (chart, news, order book, message) visible in fixed quadrants of one window; operator never alt-tabs. Maps to regatta: keep agents-table, events, work-items, spend all visible at once on 1440p — drawer overlays acceptable for drill-down but must not occlude the row that triggered them.
- **Function-code prefix entry.** `AGT <slug> <ENTER>` jumps to agent N regardless of current view. Regatta should add `a <id>`, `w <issue#>`, `e <event-kind>` jumps. Distinct from a fuzzy palette: function codes are *deterministic verbs over a known noun*; palettes are *fuzzy search over an unknown noun*.

## 2. Stripe Dashboard — state vs activity separation, drill-down, payout viz

- **State-of-the-world card row above activity stream.** Stripe stacks (a) balance / pending payouts as a fixed card row, then (b) activity below. Regatta: spend-spark + state distribution (`running` / `waiting_review` / `merged_today` / `halt`) belongs in a fixed top bar. The "is anything on fire?" question must answer above the fold.
- **Payout breakdown bar with hover-tip lineage.** Stacked horizontal bar broken by source charge; hover reveals underlying transactions. Maps to spend-spark: each segment = per-agent contributor; hover reveals agent IDs. Turns "today's $X" into "$X, of which $Y came from agent 47 still running."
- **Object-page URL pattern.** Every Stripe object (`/payments/pi_xxx`) has a stable URL. Regatta should give every agent + work-item a permanent route (`/agents/<id>`) distinct from the drawer — enables deep-linking from chat or `open` from terminal.

## 3. Sentry — error event aggregation + breadcrumb trail + similar-group recognition

- **Events fold into "issue groups" by fingerprint.** Sentry collapses 10k raw events into ~50 groups via stack-trace fingerprint. Regatta: fold events with the same `(kind, agent_role, error_class)` into one row, `seen × N, last 5m ago`, expandable. Raw scroll becomes unreadable at 50 PRs/day × ~30 events = 1500/day.
- **Breadcrumb trail in issue page.** Sentry shows the last ~50 events preceding the error. Maps to agent drawer: on halt, show breadcrumb of state-machine transitions + tool calls before the halt, not just the halt event.
- **"Similar issues" sidebar.** Sentry surfaces issues with overlapping stack frames. Regatta: when an agent halts on `prwatch.branch_diverged`, surface other recent agents that halted with the same probe — systemic regression vs one-off recognition.

## 4. Linear — keyboard shortcuts everywhere + command-K + no-mouse workflow

- **`Cmd-K` palette as universal entry.** Fuzzy-matches over issues, projects, commands. Regatta: palette over agents (id/title/slug), work items (number/title), events (kind), commands (`kill agent N`, `acknowledge halt`, `pin`, `resume`). Complements Bloomberg function codes by handling fuzzy known-by-fragment.
- **J/K row navigation + single-key action.** J/K moves focus, then `E` edit, `X` delete, etc. Regatta agents-table: J/K focus, then `K` kill, `A` acknowledge, `P` pin, `O` open GitHub, `Enter` drawer.
- **`?` reveals all shortcuts in-context.** Cheap, huge discoverability win for a self-host operator.

## 5. Datadog — log tail + metric correlation in one view

- **Timeshare cursor — chart-hover filters log view.** Hovering on the latency chart filters logs below. Regatta: hovering on spend-spark (or agent-count timeline) filters events to that bucket. Spike-investigation goes from "scroll until I find it" to "point at the spike."
- **Tail-pause toggle.** Auto-tail with one-key pause to read a frozen window. Regatta event-stream: tail by default, `space` to pause/resume.

## 6. Honeycomb — high-cardinality query + BubbleUp

- **BubbleUp — selected chart-region surfaces over-represented dimensions.** Regatta: select a halt-spike, BubbleUp surfaces which `(agent_role, work_item_label, model_id, ci_check_name)` dominate vs baseline. "Halts 8× over baseline for `verify` CI check in last 30m" without manual pivoting.
- **Trace waterfall for a single work item.** Work-item detail shows timeline waterfall of every agent attempt across that issue's life (spawn → review → rebase → merge), horizontal bars, shared time axis. Reveals retry storms + stuck rebases at a glance.

## 7. Statsig — feature-flag operator views + activity timeline

- **Per-flag activity timeline with actor + diff.** Chronological log of who toggled, what changed. Regatta: every config-changing event (`gate.toggled`, `automerge.enabled`, `lane.capacity_changed`) is a first-class timeline entry with attribution + before→after diff inline. No more git-blaming YAML to recall "did I disable this last night?"
- **Kill switch as red-styled top-bar control.** Statsig's "disable flag" is a distinct red affordance. Regatta: system-wide pause + per-lane pause as red-bordered top-bar buttons — not buried in settings.

## 8. K9s — single-screen cluster operator workflow

- **`:resource` mode switching.** `:po` pods, `:svc` services. Regatta: `:agents`, `:issues`, `:events`, `:spend`, `:halts` switch the main pane. Keyboard-first peer to Cmd-K palette.
- **Persistent footer hotkey legend.** Per-view footer updates the visible hotkeys; operators learn by reading, not memorizing. Direct self-host onboarding win.
- **Log view inline, not modal.** K9s's `l` streams pod logs inside the layout, not in a new tab. Regatta: agent drawer streams live stdout/transcript tail without leaving the dashboard.

## Ten patterns to adopt next (prioritized for one-PR builder pickup)

Ordered by throughput-gain / impl-cost ratio. Top entries are file-disjoint and parallel-dispatchable.

1. **Cmd-K palette** (Linear) — htmx overlay, fuzzy match over agents + work items + commands; commands route to existing endpoints. New: `templates/_palette.tmpl` + `static/palette.js`. Highest leverage per PR.
2. **J/K row navigation + action keys on agents table** (Linear) — `data-hotkey` attributes + one small JS file. Independent of #1.
3. **Event-stream tail-pause** (Datadog) — one client-side bool on SSE handler, `space` toggles. Two-hour PR.
4. **Sentry-style event grouping by `(kind, agent_role, error_class)`** (Sentry) — server-side fold in `dashboardEventsView`; `× N, last Xm ago`, expandable. Highest readability win as event volume grows.
5. **Persistent footer keymap legend** (K9s) — small per-view partial. No JS.
6. **Top-bar state-of-the-world card row** (Stripe) — reorder existing partials; spend + state counters into sticky top bar. Pure layout.
7. **Stable per-agent + per-work-item routes** (Stripe) — `/agents/<id>`, `/work-items/<id>` reuse drawer partials. Enables deep-linking + back-button. Refactor, not addition.
8. **Halt kill switch + per-lane pause as red top-bar controls** (Statsig) — wire existing pause endpoints to top-bar buttons + confirm dialog.
9. **Breadcrumb trail in agent drawer** (Sentry) — last N state-machine transitions + tool calls before current state; data already in `state.Event`, filter by agent.
10. **Function-code entry `a 47<Enter>`** (Bloomberg) — small parser on top of Cmd-K input; depends on #1. Last because marginal value drops once palette ships.

Deferred (real value, larger surface or speculative): Honeycomb BubbleUp, Datadog timeshare cursor (need chart-region interaction infra), Stripe payout-segment hover-tip (needs per-segment attribution not currently captured), Honeycomb work-item trace waterfall (needs cross-attempt time index). Reopen-trigger: event volume > 5000/day OR operator reports "can't find which agent caused the spend spike."

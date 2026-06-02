---
id: OBS-WAVE-D-T2
title: regatta status TUI subcommand (bubbletea, 5 panels) (item #13)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0a, OBS-WAVE-A-T0b, OBS-WAVE-A-T1, OBS-WAVE-A-T2, OBS-WAVE-A-T3, OBS-WAVE-B-T1, OBS-WAVE-B-T2, OBS-WAVE-B-T3, OBS-WAVE-B-T4, OBS-WAVE-C-T1, OBS-WAVE-C-T2, OBS-WAVE-C-T3, OBS-WAVE-C-T4
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-2 row item #13, §7 Wave-D table row D-T2, §6.1 panel-budget table (80×24 terminal), §10 R5 (Prom-unreachable banner), §7 Wave-D exit gate (< 3 s cold render).

## Task

Create new `cmd/regatta/status.go` TUI subcommand using `bubbletea` (charmbracelet/bubbletea — **add to `go.mod`**, not currently present). Renders 5 panels per spec §6.1 budget table. Drops the Triggers panel — relocated to a sibling `regatta triggers` subcommand (D-T3).

5 panels per spec §6.1:

1. **Cost** — last 1 h spend in USD by DAG (reads `regatta_cost_usd_total` via Prom HTTP API).
2. **L4 latency** — P95 over last 5 min (reads `regatta_gate_l4_latency_ms` histogram quantile).
3. **Scheduler tick** — P95 + most-recent tick (reads `regatta_scheduler_tick_latency_ms` histogram quantile).
4. **Substrate health** — events/sec + chain breaks + divergences over last 5 min (reads B-T1 / B-T2 / B-T3 counters).
5. **Adversarial findings** — last 7-d fate breakdown (reads D-T1 counter once merged; pre-D-T1 dispatch is technically possible but the panel renders zeros — coordinate dispatch ordering so D-T1 lands first).

Data source: Prom HTTP API for metrics; sqlite for cost fallback. Per spec §10 R5 + `feedback_decision_priority` (UX-first): when Prom HTTP API returns error, show banner "Prom unreachable — sqlite fallback (cost only)" instead of zeros.

**Per-panel degradation** (each metric source resolves independently — handles the 13-dep fan-in case where a subset of Wave A/B/C items merged out of order, e.g. C-T1 merged but B-T1 not yet): each panel renders one of three states — `OK` (metric series present + non-zero), `EMPTY` (metric exists but no data yet — shown as "—" with a one-line "waiting for first observation" hint), `MISSING` (PromQL `absent()` returns 1 — metric name not registered at the backend; shown as "n/a (metric not yet shipped — see Wave-X exit gate)"). Wave-D dispatch is safe even if one Wave-B/C item is in-flight; the panel shows MISSING until the upstream PR merges.

**Render budget**: < 3 s cold start on 80×24 terminal per Wave-D exit gate. `BenchmarkStatusRender_ColdStart` enforces.

**Panel-budget table** (per spec §6.1): row count ≤ 24, max column width ≤ 80. `TestStatus_FitsInDefaultTerminal` parses rendered output + asserts both bounds.

Per `feedback_research_design_principles`: bubbletea is the proven OSS choice — don't roll a custom TUI. If the panel layout fights the bubbletea model, re-spawn the design subagent before building a custom widget.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; bubbletea added to `go.mod` cleanly; 5 panels render on a real Prom+sqlite backend; B1+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 from spec §8. Prom-unreachable banner test (`TestStatus_BackendDownShowsBanner`) green per §10 R5. `TestStatus_FitsInDefaultTerminal` green per §6.1.
- **A+ (stretch):** A + `BenchmarkStatusRender_ColdStart` ≤ 3000 ms cold start (spec §8 A+3 + Wave-D exit gate); A+6 from spec §8 — panel-budget table assertions hold on 80×24 terminal (rows ≤ 24, max col ≤ 80).

## Acceptance criteria

- [planned] c1: New `cmd/regatta/status.go` registers `regatta status` subcommand using bubbletea (spec §3 item #13).
- [planned] c2: bubbletea added to `go.mod`; `go mod tidy` clean.
- [planned] c3: 5 panels render (Cost, L4 latency, Scheduler tick, Substrate health, Adversarial findings) per spec §6.1.
- [planned] c4: Prom-unreachable banner shows "sqlite fallback (cost only)" instead of zeros (spec §10 R5); per-panel `OK`/`EMPTY`/`MISSING` state resolves independently so partial Wave-A/B/C completion does not blank-screen the TUI.
- [planned] c5: Cold render budget < 3 s on 80×24 terminal; `BenchmarkStatusRender_ColdStart` proves it (Wave-D exit gate).
- [planned] c6: `TestStatus_FitsInDefaultTerminal` enforces rows ≤ 24 + max col ≤ 80 (spec §6.1).
- [planned] c7: Dispatches AFTER Waves A+B+C all merged — TUI panels reference all wave metrics.
- [planned] c8: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.

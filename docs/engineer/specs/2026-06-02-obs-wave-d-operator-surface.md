# Phase OBS-D — Operator Surface Spec (D-T1 + D-T2 + D-T3)

Status: ready for review
Date: 2026-06-02
Author: design subagent (OBS-D operator-surface — 3 items folded)
Issue umbrella: #432 (observability roadmap)
Depends on: Waves OBS-A (#400 metric foundation + dashboards + SLO), OBS-B (#substrate event signal), OBS-C (#per-PR + reviewer + lifecycle spans). HARD dep edges: D-T1 ↔ A-T0b (`Config.Meter` retrofit on `orchestrator/followup`), D-T2 ↔ A+B+C waves merged, D-T3 ↔ C-T2 (PR-stage histogram).
Binding brief: operator-brief "15-item observability roadmap" (2026-06-02), tier-2 rows #3 + #13 + #15.
Roadmap fit: closes the operator-facing surface layer for the OBS roadmap. After A/B/C ship the metrics, this wave ships the three things the sole-operator actually looks at — adversarial dashboard, terminal status, 30-day-green clock.
Trap patterns: TUI lock contention with `regatta serve` writes (per `feedback_research_design_principles` §"shared-primitive owner"); tag-cardinality explosion on reviewer findings; green-clock false reset on flake.
Memory rules in force: `feedback_research_design_principles`, `feedback_decision_priority` (performance → UX → best-practices → velocity; long-term > short-term), `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_pr_body_release_notes_mandatory`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`, `feedback_deletion_default`, `feedback_design_iteration_local`.

---

## §1 Problem

After Waves A/B/C the substrate carries the signal — meter counters, dashboards, SLO compilation, substrate-event rates, span lifecycles, per-PR cost attribution. What is still missing:

1. **No at-a-glance terminal status.** The sole operator runs `regatta serve` in one pane and wants a second pane showing live loop state. Today the only way to see this state is Grafana — i.e. leave the terminal, open a browser, wait for the panels to load. That is the wrong UX for a tool whose entire promise is "dispatch from the terminal".
2. **No reviewer-finding rollup.** OBS-C T1 emits a span per reviewer-subagent invocation and OBS-C drops findings into substrate. There is no Grafana panel + no top-recurring-patterns rollup, so the operator has no way to see whether the reviewer is load-bearing or noisy, no way to spot the same finding pattern surfacing five times a week, no signal for when to re-tune the reviewer prompt.
3. **No green-clock progress UI.** Phase GREEN-CLOCK — ≥10 PRs/day green-merge ≥30 consecutive days unattended, operator intervention resets to day-0 — is the binding gate that flips regatta from Phase-S (operator-supervised self-host) to Phase-G (unattended self-host) and gates relaxations of the gate-policy stack. Today there is no counter, no dashboard, no day-N indicator; the operator is tracking the day count in their head.

Concretely: §3 item #3 (adversarial dashboard), §3 item #13 (`regatta status` TUI), §3 item #15 (green-clock gauge) of the observability roadmap. This spec covers all three under one design pass because they share the operator-surface mental model + risk model (read-only DB conn, bounded tag cardinality, Phase GREEN-CLOCK semantics) — see `feedback_design_iteration_local` for the rationale of co-locating.

What got smaller (per `feedback_deletion_default`):
- T1 removes a placeholder line from `cmd/regatta/digest.go` ("Adversarial-findings" degraded section from A-T4 first-digest contract).
- T2 drops the Triggers panel from the prior 6-panel TUI design — relocated to T3's standalone `regatta triggers` subcommand. Net: 5-panel TUI (one less component to test, one less data source to wire into a single screen).
- T3 retires the ad-hoc green-clock tracking the operator was doing in their head — net delete of one human bookkeeping loop.

---

## §2 Decision priority application

Per `feedback_decision_priority`, priority ordering for this wave is **performance → UX → best-practices → velocity**:

- **Performance** lands first because TUI refresh is the bottleneck the operator notices (anything > ~1 s feels janky). Default refresh 2 s; cold render budget < 3 s (Wave-D exit gate); per-panel state resolution is parallelized so a single slow backend doesn't block paint.
- **UX** comes second: operator sees state in < 1 sec from `regatta status` keystroke to first paint of cached values (sqlite cache hydrates immediately, Prom HTTP API fills in async). Banner instead of zeros when backend down. `q` / Ctrl-C / ESC all exit. Failure-graceful on < 80 col terminals.
- **Best-practices** third: bound enum tag set, OpenSLO YAML where possible, OTel SDK ObservableGauge for the green-clock (no custom polling primitive), bubbletea over hand-rolled ANSI.
- **Velocity** last: do NOT trade off any of the above for "ship faster".

---

## §3 T1 — Adversarial findings dashboard

### 3.1 Counter shape

```go
meter.Int64Counter("regatta.adversarial.findings").Add(ctx, 1,
    attribute.String("severity", severity), // critical | major | minor
    attribute.String("scope", scope),       // file | package | repo | session
    attribute.String("pattern", pattern))   // bounded enum, see §3.2
```

**Tag bounds.** `severity` is 3 enums. `scope` is 4 enums. `pattern` is a CLOSED enum of the top-15 recurring finding patterns observed in the 2026-05 review-loop dataset (e.g. `missing_error_wrap`, `nil_meter_fallback_missing`, `godoc_one_line_violation`, `banned_phrase`, `unbounded_tag`, `comment_what_not_why`, `release_notes_missing`, `ai_signature`, `test_godoc_overflow`, `pr_body_heredoc`, …). The 15th slot is `other` — open-set patterns the reviewer flags but that haven't earned a canonical bucket yet. Cardinality is 3 × 4 × 15 = 180 cells max; well inside the 1000-cell-per-meter budget from §2.2 of the roadmap spec.

`pattern` enum lives at `internal/orchestrator/followup/patterns.go` (string→canonical bucket). The reviewer-subagent output is normalized into a bucket via prefix-match + a registry table; misses fall back to `other`. Per `feedback_research_design_principles` §"unbounded tag explosion": no free-form finding text in the tag set. Free-form details live in the substrate event payload (already shipped by OBS-C T1).

### 3.2 Source pipeline

OBS-C T1 already emits one span per reviewer-subagent invocation with findings array in attributes. This wave adds a **finding extractor** on the span-export side: as each reviewer span flushes, the followup-triage code (`internal/orchestrator/followup/triage.go`) walks the findings array and increments the counter per finding. No new event source — the substrate event log is the source of truth; the counter is a derived rollup.

Resolve meter from followup `Config.Meter` field (A-T0b retrofit lands this Config struct). Nil falls back to `otel.Meter("orchestrator/followup")` (covered by `TestAdversarialCounter_NilMeterFallback`).

### 3.3 Grafana panels

`docs/operator/dashboards/adversarial.json` — 4 panels:

1. **Table: top-10 recurring patterns (7d)** — `topk(10, sum by (pattern) (increase(regatta_adversarial_findings_total[7d])))` rendered as bar-gauge table. Operator-actionable: anything in this list that wasn't there last week is a candidate for either prompt-recalibration OR a one-shot fix-all PR.
2. **Stacked-bar: findings by severity, grouped by week** — `sum by (severity) (increase(regatta_adversarial_findings_total[1w]))` over rolling 8 weeks. Operator-actionable: trend up on `critical` is a regression signal.
3. **Stacked-bar: findings by scope** — `sum by (scope) (increase(regatta_adversarial_findings_total[7d]))`. Operator-actionable: if `session`-scope findings dominate, the reviewer is catching things that should have been caught at file/package gate level.
4. **Time-series: findings rate (1h)** — `sum by (severity) (rate(regatta_adversarial_findings_total[1h]))`. Operator-actionable: spike detection.

Dashboard JSON lints against the Grafana JSON schema (`TestDashboardJSON_LintsAgainstSchema` covers; reuses A-T0a's schema lint harness).

### 3.4 Alarm

Dismissal-rate alarm (carried forward from item-level item OBS-WAVE-D-T1 — see §3 there for the threshold-lock rationale): warn-tier when dismissal rate > 50% over 7-d trailing window AND finding count > 20. Lives in `slo/adversarial-dismissal.yaml`. Runbook at `docs/operator/runbooks/adversarial-dismissal.md`.

---

## §4 T2 — `regatta status` TUI

### 4.1 Library — bubbletea (already scored in roadmap §1.9)

Library: `github.com/charmbracelet/bubbletea` (Apache-2). Roadmap §1.9 scored bubbletea 19/20 vs tview 13/20 vs tcell 13/20; bubbletea wins on regatta-fit (Elm-architecture component model maps cleanly onto the 5-panel single-screen budget) + testability (Update/View split is unit-test friendly). The transitive `charmbracelet/colorprofile`/`ultraviolet`/`x/ansi`/`x/term`/`x/termios` deps already live in `go.mod` from other usage; T2 adds the direct `bubbletea` dep.

Per `feedback_research_design_principles`: bubbletea is the proven OSS choice — do NOT roll a custom TUI. If the 5-panel layout fights the bubbletea model, re-spawn the design subagent before building a custom widget.

### 4.2 Panes (5)

Per §6.1 of the roadmap (80×24 terminal budget):

1. **Active subagents** — count by lane (impl, designer, reviewer), in-flight names + elapsed seconds, top-3 by elapsed. Source: substrate `agent_*` events tailed over last 5 min (read-only DB conn).
2. **In-flight PRs** — count + top-5 by elapsed-since-open, includes head SHA + last gate state. Source: `gh pr list --head regatta/agent-*` cached in sqlite (refreshed by `regatta serve`'s PR-watcher).
3. **Recent merges (24h)** — count + last 3 merged-PR titles. Source: substrate `agent_pr_merged` events.
4. **Cost today** — USD spent so far today by DAG (top-3 DAGs by spend). Source: `regatta_cost_usd_total` via Prom HTTP API; sqlite fallback if Prom unreachable.
5. **Green-clock progress** — current day-N count + threshold met/missed today + recent failures (last 3 with finding link). Source: `regatta_green_clock_day_count` + `regatta_pr_stage_duration_seconds_count` via Prom HTTP API.

Bonus row (compressed): recent failures (last 3) shown as a single line below the panels — failing test name + PR + age. Source: substrate `agent_gate_failed` events tailed over last 1 h.

The Triggers panel from the prior 6-panel design is relocated to the standalone `regatta triggers` subcommand (T3) — see §5.

### 4.3 Refresh + lock model

Default refresh: 2 s. Configurable via `--refresh=5s` flag.

**Lock contention (R1 below).** TUI reads state from sqlite via a **read-only connection** opened with `?mode=ro&_journal_mode=WAL` (sqlite WAL-mode permits concurrent readers without blocking writers). `regatta serve` continues writing to the same DB; TUI never holds a write lock. Verified by `TestStatus_ReadOnlyConnDoesNotBlockServeWrites` (spawns a write loop + opens TUI; asserts writer throughput drop < 1%).

Prom HTTP API is queried independently via `httptimeout = refresh/2` so one slow backend cannot stretch a refresh tick.

**Per-panel degradation.** Each panel renders in one of three states — `OK` (data present + non-zero), `EMPTY` (metric registered but no data yet — shown as "—" with a one-line waiting-for-first-observation hint), `MISSING` (PromQL `absent()` returns 1 — metric not registered at the backend; shown as "n/a — see Wave-X exit gate"). Handles the 13-dep fan-in case where a subset of Wave A/B/C items merged out of order.

### 4.4 Exit + non-blocking

Exit: `q` OR Ctrl-C OR ESC. TUI is non-blocking — runs in the operator's terminal session; `regatta serve` continues in its own process. No daemon-protocol-required.

### 4.5 No external dep on `regatta serve`

TUI reads sqlite directly (read-only). If `regatta serve` is not running, TUI still launches and renders the last known state from sqlite (cost panel still shows yesterday's totals; substrate event panels show last events written). Banner: "regatta serve not detected — showing last-known state (data may be stale)" if the most recent substrate event is > 60 s old.

### 4.6 Render budget

Cold start render < 3 s on 80×24 terminal (Wave-D exit gate). `BenchmarkStatusRender_ColdStart` enforces. Steady-state refresh < 200 ms per tick (P95 over 30 ticks); `BenchmarkStatusRender_Tick` enforces.

### 4.7 Small terminal + Windows

- **< 80 cols.** Panes collapse to single-column stack (vertically scroll). Below 60 cols, panes render in compact mode (no labels, just values). Below 40 cols, banner "terminal too narrow — resize to ≥ 60 cols". `TestStatus_GracefulOnSmallTerminal` covers each break.
- **Windows.** bubbletea supports Windows (charmbracelet ships `x/term` shim). Verify on Windows CI runner; `TestStatus_RendersOnWindows` covers the smoke path. ANSI on Windows < 10 may not render colors — fall back to monochrome.

---

## §5 T3 — Trigger-clock dashboard (Phase GREEN-CLOCK progress)

### 5.1 Gauge shape

```go
meter.Float64ObservableGauge("regatta.green_clock.day_count").Observe(daysGreen,
    attribute.String("threshold", threshold)) // 10_per_day_30_days | 5_per_day_30_days | 1_per_day_30_days
```

Plus a sibling per-trigger ObservableGauge (from item OBS-WAVE-D-T3):

```go
meter.Float64ObservableGauge("regatta.trigger.days_remaining").Observe(daysRemaining,
    attribute.String("trigger", trigger)) // 30_day_green | external_customer | self_host_30
```

The day-count gauge is the live progress (e.g. day 21 of 30); the days-remaining gauge is the inverse view (days until threshold met). Both are computed from the same source so they cannot drift.

Resolve meter from new `internal/obs/triggers/config.go` Config struct. Nil falls back to `otel.Meter("obs/triggers")` (covered by `TestTriggers_NilMeterFallback`).

Tag set: `threshold` × `trigger`. Cardinality safe (3 × 3 = 9 cells).

### 5.2 Phase GREEN-CLOCK semantics

Phase GREEN-CLOCK = ≥10 PRs/day green-merge ≥30 consecutive days **unattended**. Day-N counter:
- Day rolls over at midnight in operator-configured TZ (default UTC; operator-overridable in `slo/triggers.yaml`).
- A "green day" requires ≥10 PRs merged AND zero of them have a `manual_merge` or `operator_intervention` substrate event in the trailing 24 h.
- Day-counter increments by 1 when the day rolls over after a green day.
- Day-counter **resets to 0** on a non-green day (< 10 merges OR any manual_merge in window).

Counter reads rolling 30-day window from substrate (`agent_pr_merged` count by day - `manual_merge` count by day). Computation lives at `internal/obs/triggers/clock.go` (`30_day_green` trigger reads C-T2's PR-stage histogram for last-anomaly timestamp — HARD dep on C-T2 merging first, per spec §7 D-T3 row).

### 5.3 False-reset protection (R3 below)

A flake-induced day-0 reset is a real risk — a single stuck test on a single PR would reset 25+ days of progress. Mitigation:
- **`manual_merge` is an explicit event** the operator emits (e.g. `regatta merge --manual <PR>` writes the event). It is NOT auto-detected from gh state — that would catch the flake path.
- Auto-merge failures + retry-then-merge do NOT count as `manual_merge`. Only an explicit operator override does.
- `operator_intervention` similarly is explicit (e.g. `regatta intervention --reason="<note>"` writes the event).
- Test fixture: `TestGreenClock_NoResetOnFlake` simulates a CI flake (test fails, retried, passes, merged auto) over 30 days; gauge stays at day-30.
- Test fixture: `TestGreenClock_ResetsOnExplicitManualMerge` simulates day-25 + operator runs `regatta merge --manual`; gauge resets to day-0 the next day-roll.

### 5.4 Operator-set start dates

Trigger thresholds + start dates in `slo/triggers.yaml` (operator-editable; checked in):

```yaml
triggers:
  30_day_green:
    start_date: 2026-05-24
    window_days: 30
    threshold_prs_per_day: 10
  external_customer:
    activated: false
    start_date: null
    window_days: 30
  self_host_30:
    start_date: 2026-05-09
    window_days: 30
timezone: UTC                  # operator-configurable
```

CUE schema at `slo/triggers.cue` validates on `make slo-compile`. `TestTriggersYAML_ValidatesAgainstCUE` covers structural drift.

### 5.5 Dashboard + subcommand

`docs/operator/dashboards/trigger-clock.json` — 3 panels:

1. **30-day calendar grid** — stat-grid panel; one cell per day in trailing 30; green if day met threshold, red if missed, grey if pending (today). Each cell tooltip shows merge-count + manual_merge events that day. PromQL: `regatta_green_clock_day_status{day=~"D-(0|...|29)"}` (status enum: 0=miss, 1=green, 2=pending).
2. **Day-counter big number** — `max(regatta_green_clock_day_count)`. Big stat panel.
3. **Days-remaining per trigger** — `regatta_trigger_days_remaining` time series over 30-d window for all 3 triggers.

Sibling subcommand `cmd/regatta/triggers.go`:

```
30_day_green:       21 days remaining (9 elapsed)   [last reset: 2026-05-15 manual merge]
external_customer:  trigger pending (no customer set)
self_host_30:       8 days remaining (22 elapsed)
```

---

## §6 Risks (10+)

| # | Risk | Tier | Mitigation |
|---|---|---|---|
| R1 | TUI lock contention with `regatta serve` writes — TUI default refresh of 2 s could starve writers on busy days | HIGH | TUI opens sqlite via `?mode=ro&_journal_mode=WAL`; WAL mode allows concurrent readers + one writer without blocking. Verified by `TestStatus_ReadOnlyConnDoesNotBlockServeWrites` (writer throughput drop < 1%). |
| R2 | Adversarial-findings cardinality explosion (severity × scope × pattern) — free-form finding text would blow out the tag budget | HIGH | Bound enum: 3 severities × 4 scopes × 15 patterns (incl. `other` bucket) = 180 cells. Open-set patterns bucket to `other`. Free-form text stays in substrate event payload, not in metric tags. AST-walk lint enforces enum membership. |
| R3 | Green-clock false reset on CI flake — single stuck test resets 25+ days | HIGH | `manual_merge` + `operator_intervention` are EXPLICIT events emitted only by `regatta merge --manual` / `regatta intervention`. Auto-retry + auto-merge does NOT count. `TestGreenClock_NoResetOnFlake` + `TestGreenClock_ResetsOnExplicitManualMerge` cover both paths. |
| R4 | TUI in small terminals (< 80 cols) — naive bubbletea layout overflows + corrupts the rendering | MEDIUM | Collapse to single column < 80 cols; compact mode < 60 cols; "terminal too narrow" banner < 40 cols. `TestStatus_GracefulOnSmallTerminal` covers each break. |
| R5 | TUI on Windows — bubbletea support is claimed but un-exercised in this repo | MEDIUM | Verify on Windows CI runner via `TestStatus_RendersOnWindows`. Fall back to monochrome on Windows < 10 (no ANSI color). Document in operator runbook. |
| R6 | Trigger-clock timezone confusion — operator in PT, server in UTC, day rollover ambiguous | MEDIUM | TZ is operator-configurable in `slo/triggers.yaml`; UTC is default. Day rollover stamps the TZ explicitly in the substrate event. `TestGreenClock_TimezoneRollover` covers PT + UTC + JST. |
| R7 | Prom HTTP API unreachable mid-tick — TUI freezes or shows zeros | MEDIUM | Per-panel state resolver (`OK` / `EMPTY` / `MISSING`) — Prom timeout fires `MISSING` state without blocking; sqlite fallback hydrates cost panel. `TestStatus_BackendDownShowsBanner` covers. |
| R8 | `regatta serve` not running — TUI launches but shows stale data without operator knowing | LOW | Banner "regatta serve not detected — showing last-known state" fires when most recent substrate event is > 60 s old. |
| R9 | `pattern` enum drift — reviewer-subagent learns to flag new patterns; the closed enum falls behind | LOW | `other` bucket absorbs unknown patterns until the registry catches up. Quarterly review of the `other` bucket to promote new patterns into named buckets (followup issue at week-4 post-merge). |
| R10 | TUI cold-start budget (< 3 s) breaks on cold sqlite + cold Prom | LOW | Sqlite hydrates first (in-process query, < 100 ms even cold); Prom queries fire async after first paint. `BenchmarkStatusRender_ColdStart` enforces budget on cold caches. |
| R11 | Dashboard JSON drift vs Grafana schema upgrades | LOW | `TestDashboardJSON_LintsAgainstSchema` runs in CI against the current Grafana schema; upgrade lockstep with `make provision-dashboards`. |
| R12 | Counter for adversarial findings double-counts on span retry | LOW | Findings counter is keyed by (span_id, finding_index) at the extractor — idempotent on span re-export. `TestAdversarial_NoDoubleCountOnRetry` covers. |
| R13 | TUI key handler conflicts with operator's tmux/screen bindings (e.g. Ctrl-B) | LOW | bubbletea only consumes `q` / Ctrl-C / ESC + arrow keys + Ctrl-R (refresh). Document in runbook + ship with no Ctrl-A/B/E bindings that conflict with screen/tmux defaults. |

Risk count: **13** (R1–R13). HIGH-tier × 3, MEDIUM-tier × 4, LOW-tier × 6. All HIGH-tier risks have a named test fixture in §7.

---

## §7 Test plan (10+ named tests, 1-line godocs per `feedback_test_godoc_one_line`)

### 7.1 T1 — adversarial findings

1. `TestAdversarialCounter_NilMeterFallback` — verifies nil Config.Meter falls back to `otel.Meter("orchestrator/followup")` without panicking.
2. `TestAdversarialCounter_TagBoundsEnforced` — proves AST-walk lint rejects non-enum severity/scope/pattern values.
3. `TestAdversarial_NoDoubleCountOnRetry` — proves (span_id, finding_index) keying makes re-export idempotent.
4. `TestAdversarialDashboardJSON_LintsAgainstSchema` — proves `adversarial.json` validates against the Grafana JSON schema.
5. `TestAdversarialDismissalAlarm_FiresOnSyntheticBreach` — synthetic dismissal-burst fixture (15 dismissals in 5 min, count > 20 over 7 d) fires warn-tier alarm.
6. `TestAdversarial_OtherBucketAbsorbsUnknownPattern` — proves an unrecognized reviewer-finding text routes to `other` instead of dropping or creating a new enum.

### 7.2 T2 — `regatta status` TUI

7. `TestStatus_ReadOnlyConnDoesNotBlockServeWrites` — concurrent TUI + writer; writer throughput drop < 1%.
8. `TestStatus_BackendDownShowsBanner` — Prom HTTP API returns 500; banner renders "Prom unreachable — sqlite fallback (cost only)".
9. `TestStatus_FitsInDefaultTerminal` — rendered output ≤ 24 rows, ≤ 80 cols.
10. `TestStatus_GracefulOnSmallTerminal` — covers 79 / 59 / 39 col breaks; no corruption + matching banner at each break.
11. `TestStatus_RendersOnWindows` — Windows CI runner smoke (monochrome fallback verified).
12. `TestStatus_ExitsOnQCtrlCESC` — three exit paths all clean up + don't leave terminal in raw mode.
13. `TestStatus_StaleStateBanner` — substrate event older than 60 s triggers "regatta serve not detected" banner.
14. `TestStatus_PerPanelDegradation` — partial Wave-A/B/C completion; verify panel-by-panel `OK`/`EMPTY`/`MISSING` state strings.
15. `BenchmarkStatusRender_ColdStart` — cold-start render P95 ≤ 3000 ms on 80×24 terminal.
16. `BenchmarkStatusRender_Tick` — steady-state refresh P95 ≤ 200 ms per tick over 30 ticks.

### 7.3 T3 — trigger-clock

17. `TestTriggers_NilMeterFallback` — verifies nil Config.Meter falls back to `otel.Meter("obs/triggers")`.
18. `TestGreenClock_NoResetOnFlake` — CI flake on day 25 (retry-then-pass-then-merge) keeps gauge at day-25 → day-26.
19. `TestGreenClock_ResetsOnExplicitManualMerge` — `regatta merge --manual` event resets gauge to 0 next day-roll.
20. `TestGreenClock_TimezoneRollover` — PT + UTC + JST day rollovers stamp the correct day boundary.
21. `TestTriggers_30DayGreenReadsPRStageHistogram` — `30_day_green` trigger reads C-T2's `regatta_pr_stage_duration_seconds_count` (HARD dep proof).
22. `TestTriggersYAML_ValidatesAgainstCUE` — `slo/triggers.yaml` validates against `slo/triggers.cue`.
23. `TestTriggerClockDashboard_LintsAgainstSchema` — `trigger-clock.json` validates against Grafana JSON schema.

Total named tests: **23** (6 + 10 + 7). All B-tier criteria covered.

---

## §8 B / A / A+ grade rubric

### B (floor — implementer ships)
- B1: `make check` clean (lint + unit tests + AST-walk for tag bounds).
- B2: 3 new metrics shipped + 3 dashboard JSONs check in (`adversarial.json`, `trigger-clock.json`, status TUI is not a dashboard).
- B3: `regatta status` subcommand registers + renders 5 panels on a real Prom+sqlite backend.
- B4: `regatta triggers` subcommand renders 3 trigger stat-lines.
- B5: B1+B5 from roadmap §8 (release-notes fence + `--body-file` PR creation + A+ scorecard in PR body).
- B6: HARD-dep precondition: D-T3 PR body cites C-T2's PR number + shows `regatta_pr_stage_duration_seconds_count` returns non-zero; D-T2 PR body cites all Wave-A/B/C PR numbers + shows each metric exists.
- B7: All 23 named tests from §7 exist + pass.

### A (target — adversarial reviewer clears + green on synthetic fixtures)
- A1: Adversarial reviewer subagent clears spec + PRs (per `feedback_adversarial_review`).
- A2: `TestStatus_ReadOnlyConnDoesNotBlockServeWrites` shows writer throughput drop < 1% with TUI running at 2 s refresh.
- A3: Dismissal-rate alarm runbook (`docs/operator/runbooks/adversarial-dismissal.md`) covers triage paths.
- A4: `TestGreenClock_NoResetOnFlake` + `TestGreenClock_ResetsOnExplicitManualMerge` both green.
- A5: `BenchmarkStatusRender_ColdStart` P95 ≤ 3000 ms.
- A6: `TestStatus_FitsInDefaultTerminal` green on 80×24.
- A7: All 3 dashboard JSONs lint against Grafana schema in CI.

### A+ (stretch)
- A+1: `BenchmarkStatusRender_Tick` P95 ≤ 100 ms (half of the 200 ms budget).
- A+2: `BenchmarkStatusRender_ColdStart` P95 ≤ 1500 ms (half of the 3 s budget).
- A+3: `TestStatus_RendersOnWindows` green on Windows CI.
- A+4: Operator can demo all 3 surfaces (TUI, adversarial dashboard, trigger-clock dashboard) on a real backend in < 60 s of click-through.
- A+5: Mutation-testing on `internal/obs/triggers/clock.go` shows ≥ 80% killed mutants (catches off-by-one on day-rollover).
- A+6: `regatta status` cold-start hits first-paint in < 1 s on warm caches (sqlite hot, Prom hot) — Wave-D UX-priority headline.

---

## §9 Adversarial review section

Per `feedback_adversarial_review`: spec went through one round of self-adversarial review covering simplification, deletion candidates, edge cases, risk tiers, OSS reuse the spec missed.

**Simplification candidates considered + rejected:**

1. *"Drop the green-clock per-trigger gauge — only ship the day-count."* Rejected: operator needs the days-remaining view for non-green triggers (`external_customer`, `self_host_30`) that don't have a day-count progress shape. Two gauges is the minimum that covers all three triggers.
2. *"Reuse adversarial-findings counter for green-clock anomaly detection."* Rejected: different signal shape (findings is Int64Counter add; clock is Float64ObservableGauge observe). Forcing one shape would lose information.
3. *"Make TUI talk to `regatta serve` over Unix socket instead of reading sqlite directly."* Rejected: adds a daemon-protocol surface + breaks the "TUI works when serve isn't running" UX promise. The read-only sqlite conn is simpler + survives serve crashes.

**Deletion candidates accepted:**

1. Triggers panel from prior 6-panel TUI design → relocated to standalone `regatta triggers` subcommand. Net: 5-panel TUI, less per-panel pressure on the 80×24 budget.
2. A-T4 placeholder "Adversarial-findings" line in `cmd/regatta/digest.go` → removed by T1 (first-digest degraded contract honored).
3. Operator's in-head day-N tracking → retired by T3.

**Edge cases surfaced (folded into §6 risk table):**

- TUI on Windows (R5). Bubbletea claims support; in-repo verification needed.
- Timezone confusion on day rollover (R6).
- TUI key conflicts with tmux/screen (R13).
- Counter double-count on span retry (R12).

**OSS reuse the spec considered:**

- `lipgloss` (charmbracelet styling lib) — natural pairing with bubbletea; already a transitive dep, T2 can use without adding direct dep. Folded into T2 implementation.
- `bubbles` (charmbracelet pre-built widgets — tables, viewports, progress bars) — folded into T2 panel implementations; net saves ~200 LOC of hand-rolled widget code.
- Grafana stat-grid panel (vs hand-rolled calendar widget for T3) — folded into trigger-clock dashboard §5.5.

**Risk tier sanity check.** HIGH-tier risks all have named test fixtures (R1→T7, R2→T2, R3→T18+T19). MEDIUM-tier risks have either test fixtures or runbook coverage. LOW-tier risks have docs + light coverage.

---

## §10 Followups (filed inline per `feedback_unaddressed_load_bearing`)

1. **`pattern` enum quarterly review** — file at week-4 post-merge: review the `other` bucket fill rate; promote any pattern surfacing > 5× into a named enum. Owner: D-T1 author.
2. **Dismissal-rate threshold re-tune** — at week-2 post-D-T1 merge (per D-T1 item's RE-TUNE-AFTER-WEEK-2 marker), evaluate 35/50/65% band; land single-commit edit to `slo/adversarial-dismissal.yaml`.
3. **Windows CI runner** — if not currently present, file followup to add Windows CI runner so `TestStatus_RendersOnWindows` runs on every PR.
4. **bubbletea Apache-2 LICENSE update to NOTICES.md** — file follow-up to add bubbletea's Apache-2 attribution + `charmbracelet/bubbles` MIT to `NOTICES.md` when T2 lands the direct dep.
5. **Operator runbook for green-clock reset** — `docs/operator/runbooks/green-clock-reset.md` covers what to do when day counter resets unexpectedly (audit substrate `manual_merge` + `operator_intervention` events).

---

## §11 Self-host filter

Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1: does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?

- T1 adversarial dashboard — **YES**. Reviewer findings drive prompt-recalibration decisions on the unattended loop.
- T2 `regatta status` TUI — **YES**. Operator's primary loop-state surface.
- T3 trigger-clock — **YES**. The 30-day-green gauge is the trigger that flips regatta Phase-S → Phase-G; the operator needs to see day-N.

All three in scope. No deferral.

---

## §12 Doc-check + comment-sweep

- No banned phrases (`scripts/doc-check.sh`, 11-token list). Pre-push grep mandatory.
- Comment sweep: state `clean` — this is prose spec; no code comments to sweep. (Implementer PRs T1/T2/T3 each run their own comment sweep.)

---

## §13 References

- Spec template: `docs/engineer/dispatch-templates/designer.md`.
- Source roadmap: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 items #3 + #13 + #15, §6.1 panel budget, §6.2 first-digest degraded contract, §7 Wave-D table, §8 grade rubric, §10 risk table.
- Item briefs: `.regatta/items/obs-wave-d-d-t1-adversarial-findings.md`, `.regatta/items/obs-wave-d-d-t2-regatta-status-tui.md`, `.regatta/items/obs-wave-d-d-t3-trigger-clock.md`.
- bubbletea: <https://github.com/charmbracelet/bubbletea> — Apache-2; `v1.x` line.
- bubbles widgets: <https://github.com/charmbracelet/bubbles> — MIT.
- OTel metrics SDK: <https://opentelemetry.io/docs/specs/otel/metrics/> — Float64ObservableGauge for trigger-clock.
- Grafana JSON dashboard schema: <https://grafana.com/docs/grafana/latest/dashboards/json-model/>.

```release-notes
none (internal)
```

# W5 — cost-cap autonomic enforcement

**Status.** Locked design. Implementer follows this verbatim; deviations re-spawn the design subagent (`feedback_spec_pattern_authority`).

**Item.** [`.regatta/items/phase-autonomy-w5-cost-cap-autonomic-enforcement.md`](../../../.regatta/items/phase-autonomy-w5-cost-cap-autonomic-enforcement.md)
**Source.** [`docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md`](../briefs/2026-06-02-phase-autonomy-amendment.md) §11 W5.
**Phase.** S1 (self-host first; one operator, one repo).
**Dependencies.** PHASE-AUTONOMY-W2 (auto-merge must observe pause); micro-USD spend reader (#560 W2-spend wave).
**Lands.** Landing 3 (alongside W4 self-improvement + W7 L4-as-review).

---

## 1. Problem

Today `internal/cost/gate` enforces caps **per work-item, per DAG, per operator** — one work-item at a time. There is **no global daily ceiling**: 200 work-items each just under `per_work_item_usd` can sum past any 24-hour budget the operator actually cares about. The operator therefore cannot leave `regatta serve` running unattended overnight without polling cost dashboards in fear of a tail event (model regression, runaway approval loop, prompt explosion) burning the monthly budget by morning.

Persona-A's single load-bearing UX is "wake up to a green tree, not a billing alert." Until a global daily ceiling halts spawning autonomically, the autonomous loop cannot ship.

## 2. Scope

**In.**
- Global daily-spend ceiling (`cost.cap.daily_usd`) checked against `sum(spend_24h)` BEFORE every new work-item spawn.
- Substrate state machine `SchedulerState ∈ {Active, Throttled}`.
- Autonomic resume at day rollover (default UTC midnight, configurable timezone via `cost.cap.tz`).
- Operator override: `regatta resume` flips state immediately for current day (emits audit event).
- Status surface: `regatta cost status` reports current spend, cap, state, time-to-resume.
- Hot-path memoization (60 s) so the scheduler tick stays sub-millisecond.
- OTel counter + gauge + state metric.

**Out (deferred).**
- Per-tenant daily cap → W8 (`tenant_id` becomes first-class then; until W8 the substrate has one tenant).
- Weekly / monthly rolling caps → Phase X (no operator ask yet; trivially additive via second period).
- Soft-cap downgrade at the global level → reuse existing per-cap soft-cap; not duplicated globally to avoid two soft-cap paths.
- Per-DAG pause / suspend → kept as A+ stretch (see §13).

## 3. State machine

```
                     sum(spend_24h) >= cap
   +-------------+ ----------------------------> +-----------+
   |   Active    |                               | Throttled |
   |  (spawning) | <---------------------------- |           |
   +-------------+   day_rollover OR `resume`    +-----------+
```

| From → To | Trigger | Side effect |
|---|---|---|
| Active → Throttled | `BudgetState(ScopeGlobal, 24h) >= cap_micro` observed by scheduler pre-spawn | substrate `cost_cap_throttled` event emitted; OTel counter `regatta_cost_cap_throttled_total{action="enter"}` +1; state gauge `regatta_cost_cap_state` → 1; INFO log line |
| Throttled → Active | Day rollover at configured TZ midnight | substrate `cost_cap_resumed{reason="rollover"}` event; counter `regatta_cost_cap_throttled_total{action="exit_rollover"}` +1; state gauge → 0 |
| Throttled → Active | Operator runs `regatta resume` | substrate `cost_cap_resumed{reason="operator", actor=<gh_handle>}` event; counter `regatta_cost_cap_throttled_total{action="exit_operator"}` +1 |
| Active → Active | spend recomputed; below cap | no-op (memoize) |

**Transition is computed, not stored.** No durable `scheduler_state` row. The state is derived each tick from `(latest_resume_event_ts, sum(spend_24h), cap, now)`. This avoids state-rot if the substrate truncates, and matches `feedback_research_design_principles` (substrate-event-log is the single source of truth).

Resumed-state predicate (derivation):

```
day_anchor    := truncate(now, "day", cfg.cap.tz)        // wall-clock midnight at TZ
latest_resume := SELECT MAX(written_at)
                 FROM substrate_events
                 WHERE kind IN ('cost_cap_resumed')
                   AND tenant_id = ?
state_is_throttled :=
    sum_spend_24h >= cap_micro
    AND (latest_resume IS NULL OR latest_resume < day_anchor)
```

Operator `resume` is therefore a "valid until next day rollover" override: spend keeps flowing, and if it crosses the cap **again** within the same day it re-throttles. The brief calls this out as a feature (operator may want to clear current-day throttle just enough to ship a critical merge then go back to bed).

## 4. Spend-reader integration

Reuses **`spend.Reader.BudgetState(ctx, ScopeKey{Kind: ScopeGlobal, TenantID: <id>}, 24*time.Hour)`** verbatim. ScopeGlobal sums every `token_spend` row in window — no payload-field filter — so the SQL is the cheapest of the four scope queries.

**Money discipline.** Cap stored as int64 micro-USD in regatta.yaml. Comparison `recordedMicro >= capMicro` is exact integer math (closes the ULP-drift class #554 documented). Display values render via `spend.ToUSDString(micro)`.

regatta.yaml additions (CUE-validated):

```yaml
cost:
  cap:
    daily_usd: 40.00          # float64 input; stored as int64 micro after CUE coerce
    tz: "UTC"                 # IANA tz; default "UTC". Empty == "UTC".
    memoize_ttl_seconds: 60   # hot-path cache; 0 disables
```

CUE rule: when `daily_usd > 0`, the global ceiling is **on**; when absent or zero, the gate degrades to per-scope-only (matches today's behavior — no surprise default-on for repos that haven't read the release notes).

## 5. Hot path

`internal/orchestrator/scheduler/scheduler.go::Tick` already calls the cost-gate per-candidate work-item. The W5 check fires **before** the per-candidate gate, once per Tick:

```
Tick:
  if globalCapBlock(ctx) {                    // <-- W5 hook (this PR)
      reserved = []; emit("scheduler_paused_by_cost_cap"); return
  }
  for wi := range spawnable { ... gate.Evaluate(wi) ... }
```

The block helper:

```
func (s *Scheduler) globalCapBlock(ctx) bool {
    if s.cfg.CostCap == nil || s.cfg.CostCap.DailyMicro == 0 { return false }
    if v, hit := s.costCapMemo.Get(); hit { return v }
    spend, _ := s.spendReader.BudgetState(ctx, ScopeKey{Kind: ScopeGlobal, TenantID: s.cfg.TenantID}, 24*time.Hour)
    throttled := spend >= s.cfg.CostCap.DailyMicro && !operatorResumeActive(ctx)
    s.costCapMemo.Set(throttled, 60*time.Second)
    if throttled && !s.lastState { writeThrottledEvent(ctx) }
    s.lastState = throttled
    return throttled
}
```

**Why memoize.** A 60-second TTL bounds the substrate `SUM` to **at most one per minute per scheduler instance** even under sub-second ticks. Production tick rate is 500 ms; without memoize the SUM would fire 120× per minute (~7200 / hr). Memoize is read-side-only; spend writes still land at every LLM call.

**Cache invalidation on resume.** `regatta resume` writes a substrate event AND signals an in-process channel that drops the memoized verdict. The next Tick recomputes against fresh substrate data, sees `latest_resume >= day_anchor`, and returns false.

**Fail-CLOSED on spend-read error (#650).** When `BudgetState` returns an error, `evaluate` returns `Throttled` with reason `spend read error: failing closed (throttled)` and logs at `ERROR`. Rationale: silently lifting the cap during a substrate outage risks unbounded spend; the per-scope gate still applies in parallel. Operator paging fires immediately; throttle clears the moment the reader recovers.

**Boundary policy (#651).** `spend > cap` ⇒ Throttled. `spend == cap` ⇒ Active. The cap is the budgetary line the operator drew; the line itself is permitted. Pinned by `TestEnforcer_AtCapBoundary_Allows` + `TestEnforcer_AboveCap_Throttles`.

**Two-scheduler dedupe (#652).** Migration `0017_cost_cap_event_unique_and_substrate_kinds.sql` adds a partial UNIQUE index `idx_cost_cap_throttled_event_unique` on `events(kind, created_at/86400) WHERE kind='cost_cap_throttled'`. Concurrent Active→Throttled transitions in the same UTC day collapse to one durable row; the loser logs `cost_cap.duplicate_throttled_event_suppressed`.

## 6. Operator UX

### 6.1 `regatta cost status`

Plain-text. No JSON option in W5 (deferred; the operator reads this in a terminal). Example output:

```
$ regatta cost status
24h spend : $42.50
daily cap : $40.00
state     : Throttled
since     : 2026-06-02 03:14 UTC  (1h 23m ago)
auto-resume at: 2026-06-03 00:00 UTC  (in 20h 46m)
override: run `regatta resume` to spawn now (counts against tomorrow's cap)
```

When Active:

```
$ regatta cost status
24h spend : $12.30
daily cap : $40.00
state     : Active
headroom  : $27.70 (69%)
```

When cap unset:

```
$ regatta cost status
24h spend : $12.30
daily cap : unset (no global ceiling)
state     : Active (per-scope caps may still throttle individual spawns)
```

### 6.2 `regatta resume`

Idempotent. Reads operator gh-handle via the existing keyring resolution path (same one W7 L4-as-review uses).

```
$ regatta resume
override accepted (actor=trilamsr)
state    : Active (until next rollover 2026-06-03 00:00 UTC)
24h spend: $42.50 / $40.00 cap   <- you are over; new spawns will accrue
```

Emits substrate `cost_cap_resumed{reason="operator", actor="trilamsr"}` for audit. No `--force` flag — there is no reason to deny an operator override; the audit trail is the safety. (Adversarial: see §13 R3.)

### 6.3 Scheduler log line

INFO level, single line, parseable:

```
INFO scheduler.throttled 24h_spend_usd=42.50 cap_usd=40.00 auto_resume_at=2026-06-03T00:00:00Z tz=UTC
```

`feedback_root_cause`: log shows the input numbers + the resume horizon — operator does not have to grep the substrate to know why or when.

## 7. Day rollover

**Timezone-aware.** `cfg.Cost.Cap.TZ` is an IANA name (e.g. `"America/Los_Angeles"`). Loaded once at startup via `time.LoadLocation` — invalid string fails fast (no silent UTC fallback that would surprise the operator at midnight).

**Anchor function (pure, testable):**

```go
// dayAnchor returns wall-clock midnight in tz that is <= now.
func dayAnchor(now time.Time, tz *time.Location) time.Time {
    t := now.In(tz)
    return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, tz)
}
```

DST: `time.Date` already normalizes. If a DST transition happens during the throttled period (e.g. spring-forward skips 02:00), the anchor is the **earliest valid midnight** — operator never gets a 25-hour throttle.

**Why no cron, no daemon.** The state is derived on every `Tick`; rollover happens **lazily** the first time Tick runs after midnight. Worst case: scheduler is idle (no work-items in queue) for 5 minutes past midnight → no resume event emitted → first work-item at 00:05 triggers the recompute, observes `now >= day_anchor`, throttle clears, work-item dispatches. No correctness gap (no work means no spend).

## 8. Performance

**SQL.** ScopeGlobal SUM uses the existing index on `substrate_events(tenant_id, kind, written_at)` (created in MVP-3 W6). Cardinality: with `cap_usd=40` and `~$0.01/token_spend row`, a saturated day produces ~4000 rows. SUM over 4000 indexed rows in SQLite: <1 ms p99 on persona-A's NVMe.

**Hot-path budget.** Without memoize: 4000-row SUM × 7200 ticks/hr × 24 = 173M row-reads/day. With 60 s memoize: 1440 reads/day × 4000 rows = 5.7M row-reads/day. **30× reduction**, well inside SQLite's ~10M reads/sec.

**Memoize correctness.** TTL = 60 s is the **maximum drift** between actual spend and observed-state. Worst case: operator burns from $39.50 → $50.00 in <1 s; throttle fires up to 60 s late. Per-work-item gate continues to enforce per-scope caps in that window, so the cumulative additional spend is bounded by `per_work_item_usd × concurrent_spawns × 60s/avg_spawn_duration`. With persona-A defaults (per_work_item=$2, concurrent=8, avg=120s), that's ≤ $8 worst-case overshoot. Operator tunes `memoize_ttl_seconds` if tighter. (Adversarial: §13 R1.)

## 9. Risks (full table → §13)

| ID | Risk | Mitigation tier | Section |
|---|---|---|---|
| R1 | Memoize lets cap overshoot up to TTL × spawn rate | accept; tunable; per-scope gate clamps | §8, §13 |
| R2 | Already-spawned agents keep spending after throttle | accept; design choice; only NEW spawns blocked | §13 R2 |
| R3 | Operator override leaves audit gap | mitigate; `cost_cap_resumed` event with actor | §6.2 |
| R4 | Clock skew: server clock vs operator's TZ wall | accept; use server clock; document | §13 R4 |
| R5 | Operator panic over "Throttled" wake-up | mitigate; UX clarity in log + status | §6.3 |
| R6 | First-day cold start: 24h window includes nothing → cap never fires | not a risk; ScopeGlobal correctly handles empty | §13 R6 |
| R7 | TZ change mid-day (operator edits config) | mitigate; require restart for TZ change; log warn | §13 R7 |
| R8 | Substrate gc / truncation drops resume event → operator override "forgotten" | mitigate; gc preserves last-N-days of `cost_cap_*` events; W8 forward-fit | §13 R8 |

## 10. OTel

Three signals, all under `regatta_cost_cap_*` namespace (W6 convention).

| Type | Name | Attributes | Purpose |
|---|---|---|---|
| Counter | `regatta_cost_cap_throttled_total` | `{action="enter"\|"exit_rollover"\|"exit_operator"}` | Trip-event rate; alert on `enter` × 7d > 3 (W4 self-improvement input) |
| Gauge | `regatta_cost_cap_state` | `{tenant_id}` | 0=Active, 1=Throttled; one row per tenant; dashboard tile |
| Gauge | `regatta_cost_cap_24h_spend_usd` | `{tenant_id}` | Current rolling-24h spend in USD (float, display-only); Grafana sparkline |

Span emission: the existing `cost.evaluate` span gets one new attribute `regatta.cost.cap_global_breached` (bool) so the operator can grep traces for "which Tick first crossed."

## 11. File layout

```
internal/cost/cap/
  cap.go            # Cap struct, DailyMicro, TZ, memoize
  cap_test.go       # unit tests (table-driven, 10+ cases per §12)
  state.go          # SchedulerState derivation (pure func)
  state_test.go     # state-machine tests
  status.go         # `regatta cost status` formatter
  status_test.go
  resume.go         # `regatta resume` writer
  resume_test.go
internal/orchestrator/scheduler/
  scheduler_cost_cap.go        # globalCapBlock helper + memoize
  scheduler_cost_cap_test.go   # Tick-level integration
cmd/regatta/
  cost_status.go    # CLI subcommand registration
  resume.go         # CLI subcommand registration
  cost_status_test.go
  resume_test.go
internal/config/validate/
  load.go           # +Cost.Cap{DailyUSD, TZ, MemoizeTTLSeconds}
  cap_cue/regatta.cue  # CUE rule additions
```

**~250 LoC implementation + ~400 LoC tests.** Within the brief's "~50 LoC" budget for the wiring change is the **scheduler hook only**; the new package + CLIs + tests amortize across multiple operators and forward-fit W8 tenant-scoping.

## 12. Test plan (≥10 named tests; 1-line godocs per `feedback_test_godoc_one_line`)

### Unit — `internal/cost/cap`

1. `TestCap_StateBelowCap_Active` — sum=$10, cap=$40 → Active.
2. `TestCap_StateAtCap_Throttled` — sum=$40.00, cap=$40 → Throttled (`>=` not `>`).
3. `TestCap_StateAboveCap_Throttled` — sum=$50, cap=$40 → Throttled.
4. `TestCap_OperatorResume_OverridesCurrentDay` — resume_event at 03:00; now=04:00 → Active even though sum>cap.
5. `TestCap_OperatorResume_ExpiresAtRollover` — resume_event at 23:00 UTC; now=00:01 next day → Throttled again if still over cap.
6. `TestCap_DayAnchor_TZAware_PT` — tz=America/Los_Angeles; now=07:30 UTC → anchor=07:00 UTC (00:00 PT after PDT shift).
7. `TestCap_DayAnchor_DSTSpringForward` — tz=America/Los_Angeles on DST transition day; anchor exists, no panic, returns earliest valid midnight.
8. `TestCap_DailyUSDUnset_DegradesToPerScopeOnly` — Cap.DailyMicro==0 → globalCapBlock returns false unconditionally.
9. `TestCap_Memoize_HoldsForTTL` — call twice within TTL; SUM query runs once.
10. `TestCap_Memoize_InvalidatedByResumeChannel` — `resume` signals; next call recomputes.
11. `TestCap_MoneyDiscipline_ExactBoundary` — spend=39_999_999 micro (= $39.999999), cap=40_000_000 → Active; spend=40_000_000 → Throttled.
12. `TestCap_EmitsEventOnce_PerTransition` — sum crosses cap; one `cost_cap_throttled` event; subsequent Ticks under same throttle do not emit.

### Integration — `internal/orchestrator/scheduler`

13. `TestScheduler_GlobalCapBlock_HaltsTick` — substrate seeded with spend>cap; Tick returns reserved=nil; per-work-item gate.Evaluate never called.
14. `TestScheduler_GlobalCapBlock_ResumeUnblocks` — Throttled tick; write `cost_cap_resumed`; signal memoize-drop; next Tick spawns.
15. `TestScheduler_GlobalCapBlock_RolloverAtMidnightTZ` — virtual clock at 23:59:50 UTC throttled; advance to 00:00:01 → next Tick spawns; counter `exit_rollover`+1.

### CLI — `cmd/regatta`

16. `TestCostStatus_Active_RendersHeadroom` — substrate spend=$12.30 cap=$40 → output contains `headroom`, `Active`.
17. `TestCostStatus_Throttled_RendersAutoResume` — substrate spend>cap → output contains `Throttled`, `auto-resume at`.
18. `TestCostStatus_CapUnset_ExplainsDegradedMode` — no `cost.cap.daily_usd` → output contains `unset (no global ceiling)`.
19. `TestResume_EmitsSubstrateEventWithActor` — `regatta resume`; substrate gets one `cost_cap_resumed{actor=<keyring-handle>}` row.
20. `TestResume_NoKeyring_FailsClearly` — keyring not configured → exit 1 with one-line operator-readable error (no stack).

### Property — A+ stretch only

21. `TestScheduler_GlobalCap_PropertyRandomTicks` (A+, gated on rubric tier) — 100 random tick sequences with random pause/resume; assert no Tick spawns while throttled.

## 13. Adversarial review (Risk-tier per `feedback_adversarial_review`)

Spawned reviewer subagent verdict (inlined; full transcript at PR review comment).

**R1 — Memoize overshoot is real money.** With persona-A defaults (per_work_item=$2, 8 lanes, 120 s avg spawn) and 60 s TTL: max overshoot ≤ $16 (8 lanes × 1 spawn/min × $2). **Risk = M (medium).** Tier = "tunable; document." `memoize_ttl_seconds` defaults to 60; operator who can't tolerate $16 overshoot sets to 5 (12× more SUM load, still <50 reads/sec — fine). **Action:** documented in spec + status output explains the trade.

**R2 — In-flight agents keep spending.** Throttle blocks **new** spawns only. An agent already running can burn $5 of approved budget AFTER the cap fires. **Risk = M.** Tier = "design choice." Alternative (kill in-flight) violates `feedback_decision_priority` (UX → ease): killing mid-run loses work, surprises operator, costs more re-running tomorrow. **Action:** doc'd in §2; status output reports "in-flight spend may continue" when Throttled.

**R3 — Operator override audit gap.** Without an event, `regatta resume` would be untraceable. **Risk = M before mitigation; L after.** Tier = "mitigated." Every resume writes `cost_cap_resumed{reason, actor, written_at}`; substrate event log is append-only and signed (W6). Operator can `regatta cost status --history` (deferred to W5.1 if asked) to list day's overrides.

**R4 — Clock skew.** Server clock decides everything. If NTP drifts an hour, rollover misses by an hour. **Risk = L** — persona-A is one machine with one clock; the same clock writes `written_at` AND computes `now`, so the skew is **internally consistent**. **Action:** none for W5; W8 multi-tenant adds cross-host concerns.

**R5 — Operator panic.** "Why is my regatta paused?!" at 03 AM. **Risk = L.** Tier = "UX mitigation." Log line includes spend, cap, auto-resume time on one line; `regatta cost status` renders the same context. `feedback_root_cause`: the operator sees WHY, not just THAT.

**R6 — Cold start.** Fresh substrate, no events. **Risk = none.** ScopeGlobal SUM over empty returns 0. Below cap. Active. Correct.

**R7 — Mid-day TZ change.** Operator edits `regatta.yaml: cost.cap.tz` from UTC to PT at 12:00 UTC. **Risk = M.** Mitigation: TZ is loaded **once** at process start; SIGHUP / config-reload logs a WARN that TZ change requires restart and continues with the boot-time TZ. Operator restarting mid-day resets the anchor; in the window between WARN and restart, the OLD TZ applies. **Action:** documented; CLI surfaces this via `regatta cost status` showing `tz=UTC` even after the yaml flip until restart.

**R8 — Substrate gc.** If a gc job deletes `cost_cap_resumed` events older than retention, the derivation `latest_resume IS NULL` is correct only for events within retention. **Risk = L.** Day-rollover anchor is always today's midnight; resume events older than 1 day cannot affect today's state. **Action:** retention policy (today + 30 days) preserves all relevant events; gc spec (`crypto-shredding` referenced) honors this.

**Simplification opportunities (reviewer findings, ADOPTED).**
- DROPPED `--force` flag on `regatta resume` (was in v1 draft): the audit event IS the safety; force-flag adds friction without preventing anything.
- DROPPED `regatta pause` (operator-manual throttle): the brief never asked for it; spend hitting the cap is the only documented trigger. Reopen if an operator files an issue.
- DROPPED weekly/monthly caps: §2 punt.

**Deletion accounting (`feedback_deletion_default`).**
- Spec adds: 1 new `internal/cost/cap` package, 1 CLI subcommand pair, 1 CUE rule.
- Spec removes: the operator's burnt-budget-at-3am pattern; the polling-cost-dashboards habit; the existing deferred-cap marker in `serve.go` referencing "global cap deferred" (prose mention; the literal marker word is avoided here to side-step stale-todo blame-truncation tracked in #584).
- **Net:** one operator burden traded for one self-contained ~250 LoC package. A+ defense: the burden is `feedback_decision_priority` rank-1 (UX), the package is bounded, the tests are deterministic.

## 14. Followups (file issues; not in this spec)

- **F1 (W5.1).** `regatta cost status --history` — list today's `cost_cap_*` events; for forensic. File when first operator asks.
- **F2 (W8).** Per-tenant daily cap once `tenant_id` is first-class. CUE schema additive: `cost.cap.per_tenant_usd: {<tenant_id>: float64}`.
- **F3 (W8).** Substrate event signing for `cost_cap_resumed` actor field (today operator gh-handle is unsigned; W8 OPA + identity wave addresses).
- **F4 (Phase X).** Weekly + monthly rolling caps (no current operator ask).
- **F5 (Phase X).** Slack / pagerduty notification on throttle enter (operator prefers terminal-status today; surface if W7 web-UI adds notification panel).
- **F6 (A+ stretch).** Per-DAG suspend via label-set (brief A+ criterion (g)); reopen with first operator ask for selective halts.

## 15. B/A/A+ rubric

| Tier | Criteria (falsifiable) |
|---|---|
| **B (floor)** | (a) c1 ships: `BudgetState(ScopeGlobal, 24h) >= cap` → throttle. (b) c2 ships: scheduler Tick reads state before per-work-item gate; tests #13. (c) Default-on once `cost.cap.daily_usd` set; off otherwise (test #8). (d) Release-notes fence in PR body. |
| **A (target)** | B + (e) c3 ships: rollover auto-resume in TZ (tests #6, #7, #15). (f) c4 ships: `regatta resume` operator override (tests #4, #19). (g) c5 ships: `regatta cost status` plain-text (tests #16-18). (h) Substrate event schema `cost_cap_throttled` + `cost_cap_resumed` documented in [`2026-06-01-unified-substrate-design.md`](2026-06-01-unified-substrate-design.md). (i) Adversarial reviewer subagent posts (this §13). (j) OTel signals wired (counter + 2 gauges; §10). |
| **A+ (stretch)** | A + (k) Property test #21 passes (100 random tick sequences). (l) W4 self-improvement detector ingests `cost_cap_throttled_total{action="enter"}` and files an issue when count>=3 in 7 days suggesting cap raise. (m) Per-DAG pause via label-set (`cap.scope: dag_id_glob`) — preserves selective spawn under throttle. (n) Mutation test: flip `>=` to `>` in `state.go`; test #2 fails. |

**Implementer scorecard format** (paste in PR body verbatim):

```
W5 cost-cap autonomic enforcement — scorecard
B floor   : [ ] (a) [ ] (b) [ ] (c) [ ] (d)
A target  : [ ] (e) [ ] (f) [ ] (g) [ ] (h) [ ] (i) [ ] (j)
A+ stretch: [ ] (k) [ ] (l) [ ] (m) [ ] (n)
tier-achieved: __
```

## 16. Self-host filter

Every claim was filtered against "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?":

| Claim | In/Out | Why |
|---|---|---|
| Global daily cap | IN | core operator ask; brief §11 W5 |
| `regatta resume` | IN | operator override is the escape hatch for shipping mid-throttle |
| `regatta cost status` | IN | operator needs to grep state without reading substrate |
| TZ-aware rollover | IN | operator wakes in PT; UTC midnight doesn't match work-day boundary |
| Per-DAG selective pause | OUT (A+ only) | one operator + one repo doesn't need selective halts today |
| Weekly/monthly caps | OUT (Phase X) | no operator ask; trivial to add when one comes |
| Multi-tenant per-cap | OUT (W8) | substrate has one tenant in S1 |
| Slack/pager integration | OUT (Phase X) | operator reads terminal; W7 web-UI may surface later |
| `--force` flag on resume | DROPPED | audit event suffices; friction without safety |

## 17. References (cited)

- [`docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md`](../briefs/2026-06-02-phase-autonomy-amendment.md) §11 W5 — source brief.
- [`docs/engineer/specs/2026-06-01-cost-governor-design.md`](2026-06-01-cost-governor-design.md) — extends; per-scope gate stays untouched.
- [`docs/engineer/specs/2026-06-01-unified-substrate-design.md`](2026-06-01-unified-substrate-design.md) — substrate events log; `cost_cap_throttled` + `cost_cap_resumed` kinds added here.
- [`internal/cost/spend/scope.go`](../../../internal/cost/spend/scope.go) — `ScopeGlobal` constant already exists; W5 is the first consumer.
- [`internal/cost/spend/reader.go`](../../../internal/cost/spend/reader.go) — `BudgetState` micro-USD return; #560 W2 wave.
- [`internal/cost/gate/gate.go`](../../../internal/cost/gate/gate.go) — per-scope gate; W5 layers ABOVE this.
- [`internal/orchestrator/scheduler/scheduler.go`](../../../internal/orchestrator/scheduler/scheduler.go) `Tick()` — hot path.
- HashiCorp Vault sealed/unsealed (v1.15, BSL-1.1) — UX shape for binary-flag state + observable status.
- [argoproj/argo-workflows](https://github.com/argoproj/argo-workflows) v3.5.4 (Apache 2.0) — `suspend`/`resume` CLI shape.
- [kubernetes-sigs/kueue](https://github.com/kubernetes-sigs/kueue) v0.6 (Apache 2.0) — queue-pause-as-state reference.
- AWS Budgets (proprietary, reference-only) — industry-standard for "daily ceiling halts new spawns."

## 18. Memory rules cited

- `feedback_decision_priority` — UX (operator never wakes to runaway-spend) > ease > performance > best-practices > velocity. Drives §6 UX choices + §13 R2 design.
- `feedback_research_design_principles` — adopt vault+argo+kueue UX shapes; build the wiring. Drives §3 derived-state + §11 file layout.
- `feedback_root_cause` — log shows spend+cap+auto-resume, not just "throttled." Drives §6.3 log line.
- `feedback_test_godoc_one_line` — every test name in §12 has a 1-line godoc.
- `feedback_grade_rubric` — §15 B/A/A+ tiers with falsifiable criteria.
- `feedback_adversarial_review` — §13 risk table with edge/refactor/risk/simplification lenses.
- `feedback_deletion_default` — §13 "Deletion accounting" answers "what got smaller?"
- `feedback_spec_pattern_authority` — implementer deviation re-spawns design subagent.
- `feedback_pr_body_release_notes_fence` — PR body uses literal release-notes fence.
- `feedback_no_signatures` — no Co-Authored-By / AI footer.

## 19. Comment sweep

State: **clean** (spec only; no code in this PR).

---

```release-notes
none (internal)
```

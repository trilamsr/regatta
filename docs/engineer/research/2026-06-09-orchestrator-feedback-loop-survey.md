# Orchestrator feedback-loop survey (2026-06-09)

End-to-end trace of one work-item from `github_issues` adapter `List()` to PR merge. Pipeline mapped against `internal/orchestrator/orchestrator.go:106-150` (`Run` loop). Three tickers drive everything:

- `PollInterval` = 30s default → `PollOnce` → `AdapterSync.Sync` (`orchestrator_config.go:56`).
- `TickInterval` = 5s default → `ScheduleOnce` + `RouteRejections` + `ReapTerminal` + `WatchPRs` (`orchestrator_config.go:59`).
- `HeartbeatInterval` = 60s default → `Heartbeat` (`orchestrator_config.go:62`).

Three external workers fan out from there: spawner child goroutine (per agent), merge worker (queue-drain), and substrate JSONL append.

## Stage-by-stage

### 1. Adapter `List()`
- Fn: `(*adapter).List` — `internal/orchestrator/adapter/githubissues/adapter.go:97`. Span `adapter.github_issues.list` opens; `gh` paginated list + per-issue body parse + `warnSkip` for bad `[ID-PREFIX]`.
- Time: dominated by `gh` round-trips (1-3s typical, up to 30s on rate-limit). Span captures wall-time; no `duration_ms` slog field.
- Emits: WARN `adapter.item_skipped`, `adapter.duplicate_id`, `adapter.body_edit_failed`. Counter `regatta.adaptersync.adapter_poll.errors_total` on error path only.
- LOST: success-path `adapter.list.completed` event with item count + latency does NOT exist. Per-item-skipped reason counts not aggregated. Span ends silently on success.
- Parallelism: serial gh pagination (gh-CLI seam). Could parallelize per-repo if multi-repo later.

### 2. `adaptersync.Sync` → `BatchUpsertWorkItems`
- Fn: `(*Syncer).Sync` — `internal/orchestrator/adaptersync/adaptersync.go:122`. Span `adaptersync.sync`. `MinPollInterval` gate (line 131) short-circuits inside budget window.
- DB writes: `(*DB).BatchUpsertWorkItems` — `state/work_items_batch_upsert.go:28` (single `INSERT ... ON CONFLICT` tx, post-#89). `TombstoneBySource` after.
- Time: 10-200ms typical (sqlite local).
- Emits: WARN `adapter.empty_list`, `adapter.tombstoned`. NO substrate event on success; NO `work_item.created` / `work_item.updated` per-item events.
- LOST: zero observability into per-item upsert outcome. Dashboard cannot show "12 issues mirrored at 03:07:12"; it must `SELECT *` from `work_items` and diff. No churn metric (created vs updated vs unchanged).
- Parallelism: serial (single tx, by design — atomicity over throughput).

### 3. `ScheduleOnce` + scheduler `Tick`
- Fn: `(*Orchestrator).ScheduleOnce` — `orchestrator_schedule.go:32` (span `tick`) calls `(*Scheduler).Tick` — `scheduler/scheduler.go:306`. Step loop at scheduler.go:317-405 runs gates SERIALLY: `recheck` → `eval_edges` → `reaper` (`ExpireStaleLocks`) → `gate_l0` (`ListSpawnable`) → `gate_cost_cap` → `gate_approval` → `gate_cost` → `gate_l4` → `dispatch`.
- Time: histogram `regatta.scheduler.tick.step_duration_ms` per step (`scheduler.go:274`). Healthy tick 5-50ms; gates that hit `gh` (l4) can blow to seconds.
- Emits: `tick.started`, `tick.completed` slog (`orchestrator_schedule.go:50-52`), per-step histogram, `gate_rejected` audit rows via `applyApprovalGates` / cost gate. `EventReserveCompleted` per reserved agent.
- LOST: `gate_l0` ListSpawnable result count not directly emitted as a metric — only inferable from `EventEvaluated` attr. Per-gate REJECT counts not faceted: cost vs l4 vs approval rejections share one slog stream without a `gate=` attribute on every reject path. Recheck-backoff suppression silently drops work items each tick (`scheduler.go:159`) with no per-item visibility.
- Parallelism: steps are sequential by design (each filters the slice fed to the next). Per-work-item reservation inside `reserveFromSpawnable` (`scheduler_spawn.go:21`) loops serial — single sqlite writer.

### 4. `ScheduleOnce` → `spawner.Spawn`
- Fn: `(*Orchestrator).ScheduleOnce` builds `spawner.Request` (orchestrator_schedule.go:80-115) and calls `(*ClaudeSpawner).Spawn` — `spawner/claude.go:152`. Inside: `wm.Create` (worktree git-add + checkout, 200-800ms), `Prompt(req)` build, `starter` (`exec.Cmd` start), `cmd.Wait` goroutine + ParseStream goroutine.
- Time: synchronous Spawn returns in ~1s (worktree create). Child runs minutes-hours; `operator_invocation` span end happens in goroutine.
- Emits: `spawn.started`, `spawn.completed`, `spawn.failed`, `agent.exited` slog (with `duration_ms`, `exit_code`, `exit_reason`, `last_text_fingerprint` — `obs/events.go:35-46`). `operator_invocation` OTel span + `llm_call` children via ParseStream. On failure path orchestrator_schedule.go:99 also writes substrate `spawn_failed` event.
- LOST (#1093): NO backoff state — same `BUG-XXXX` retried every 5s tick after `spawn.failed`, no `spawn_attempts` column, no terminal `failed` state for the work_item. Worktree-create errors (e.g. "not a git repository") burn `gh` quota indefinitely. Per #1093 acceptance, fix is `spawn_attempts + exponential 5s/30s/2m/10m/1h then terminal failed`.
- LOST (#1094): stub→claude swap leaves orphan worktree; spawner.Spawn does not reconcile pre-existing `agent_<id>` dirs.
- Parallelism: spawner is per-agent goroutine (already parallel up to lane caps). Worktree creation is the serial bottleneck per-agent (git lock on primary).

### 5. Claude subprocess
- Interface: stream-json on stdout → `ParseStream` (`spawner/claude.go:166`) emits `llm_call` spans as children. `lastTextRing` ring buffer captures trailing stdout for `agent.exited` fingerprint.
- Time: minutes to hours.
- LOST: no live heartbeat from subprocess back to orchestrator beyond stdout parsing. Dashboard cannot show "agent 42 in turn 7/N" without re-reading stream-json. Stdout parse errors are swallowed (`_ = ParseStream`).

### 6. `prwatch.Sweep`
- Fn: `(*Watcher).Sweep` — `prwatch/prwatch.go:245`. Called from `(*Orchestrator).WatchPRs` (`orchestrator_prwatcher.go:23`) on every `TickInterval` (5s). Span `prwatch.sweep`. Per-agent: `gh pr list --head <branch>` + impersonator filter + state transition.
- Time: 1 `gh` call per `running`/`pr_open` agent per 5s tick. With 10 agents = 10 gh calls / 5s = 2 RPS sustained.
- Emits: `prwatch.list_failed`, `prwatch.branch_renamed_by_agent` (closes #1047), `prwatch.branch_diverged` (closes #1051), `agent_branch_renamed` substrate, `agent_pr_dirty` once-per-DIRTY-cycle.
- LOST: no aggregate `prwatch.sweep.completed` with total gh calls + per-agent dt. Per-agent errors swallowed at `sweepOne` (`prwatch.go:264`) without `agent.id` mention in span attrs. `pickPR` ambiguous-head warning fires only once per `agentID` (state leak across sweeps — `prwatch.go:148` "lastDiverged" map).
- Parallelism: serial loop. Could parallelize per-agent with a 5-worker pool — biggest gh-bound win.

### 7. Checks poller + 8. Merge coordinator
- `checks.Poller.Poll` (`checks/poller.go:44`) emits `Emission` on rollup change only; first-observation always emits, no metric on emit-frequency.
- `merge.Coordinator` runs `GatesRunning → AwaitingMerge` tx (`coordinator.go:59`). `merge.Worker.Run` (`worker.go:70`) drains queue serially. `GhProber.Probe` (`prober.go:41`) shells `gh pr view` with 30s timeout per probe.
- LOST: post-automerge CI flake leaves PR in OPEN/BLOCKED (`feedback_watch_pr_until_merged`); no `merge.stalled_after_automerge` event.

### 9. Reaper
- Fn: `(*Reaper).ReapAll` — `reaper/reaper.go:179`. Span `reaper.sweep` (standalone, not under `tick`). Called from `o.ReapTerminal` each 5s tick.
- LOST: skipped-vs-reaped ratio not surfaced as a metric; only `reap.killed` / `reap.skipped` / `reap.candidate_detected` slog events.

## Per-stage: can the dashboard render this tonight?

| Stage | Dashboard observable? | Latency to render |
| --- | --- | --- |
| 1 adapter.List | NO (no completed event) | n/a |
| 2 work_items upsert | INDIRECT (SELECT * + diff) | poll interval (5s) |
| 3 scheduler Tick | YES (`tick.started/completed`) | 5s |
| 4 spawner Spawn | YES (`spawn.*`, `agent.exited`) | 5s |
| 5 claude subprocess | PARTIAL (`llm_call` spans, no live progress) | end-of-turn |
| 6 prwatch Sweep | YES (substrate events) | 5s |
| 7 checks poll | YES (`Emission` substrate) | on-change |
| 8 merge worker | PARTIAL (no automerge-stall event) | 5s |
| 9 reaper | YES | 5s |

`internal/web/dashboard.go:425` already routes `agent.exited`, `spawn.started/completed/failed` — dashboard is observation-driven (slog tail).

## 5 highest-leverage instrumentation gaps

1. **`adaptersync.sync.completed`** with `items_total`, `created`, `updated`, `tombstoned`, `skipped_by_reason`, `duration_ms`. Without this the operator cannot tell whether a 30s poll cycle did useful work. Single most observable per-cycle artifact missing.
2. **Per-gate REJECT counter + attr**: `regatta.scheduler.gate.rejected_total{gate=approval|cost_cap|cost|l4|recheck_backoff}`. Today the rejections are in slog under different keys; cannot build a "why is item X not spawning" view in <O(log-scan).
3. **`spawn_attempts` column on work_items + `spawn.backoff_scheduled` event** (per #1093). Tonight's soak loses ~360 spawns/min/30-items when credit-balance halts.
4. **`prwatch.sweep.completed`** with `agents_checked`, `gh_calls`, `transitions{to_pr_open,branch_lost,branch_diverged}`. Dashboard cannot show PR-watch health without this; per-agent `sweepOne` swallows errors silently.
5. **`work_item.upserted` substrate event per item** with `source`, `delta=created|updated|noop`. Pairs with #1 above for per-issue timeline. Lets dashboard render a Kanban-style flow with mirror-time as the seed timestamp instead of inferring from agent rows.

## 3 wasted-work surfaces (wall-clock)

1. **#1093 spawn.failed retry**: same item retried every `TickInterval` (5s) forever. At 30 failing items × 12 attempts/min × 800ms worktree-create = **~5 min wall-clock burned per real-minute**. Plus rate-limit risk on `gh` if backed by GHCLI worktree probes. Highest steady-state cost during credit-balance halt.
2. **prwatch serial per-agent gh fan-out**: 10 agents × 1 `gh pr list` per 5s tick = 120 calls/min minimum. Each `gh` shellout is ~300-700ms; sweep wall-clock 3-7s of every 5s tick at N=10. Parallelizing 5-wide cuts to **~0.7-1.5s per sweep**, freeing the tick loop for scheduler work.
3. **recheck-backoff suppression invisible churn**: `scheduler.go:159` re-scans the spawnable set every tick to decide suppression. ListSpawnable runs unconditionally first (gate_l0) and the suppression filter discards. Whole `gate_l0`+`gate_approval` cost on perma-suppressed items × tick cadence. With 50 suppressed items + 50-200ms per gate pass = **~5-20% of tick budget wasted**. Pre-filter suppressed at the `gate_l0` SQL level.

## 2 latency cliffs (>5s by design)

1. **Tick→scheduler granularity (5s)**: every cross-tick stage (poll→scheduler dispatch, prwatch sweep→merge coord, reaper detection→cleanup) eats one full `TickInterval` minimum. A newly mirrored work_item waits up to 5s before scheduler sees it (or up to 30s when `PollInterval` is the bottleneck before mirroring even happens). End-to-end issue→spawn is **30s + 5s = 35s P99** with default config.
2. **Merge prober timeout (30s)**: `merge/prober.go:41` sets `timeout: 30 * time.Second` per `gh pr view` call. The merge `Worker.Run` drains queue serially — one slow probe stalls all subsequent merges by up to 30s. Tail latency for PR-merge is bounded by Σ(probe-timeouts) in the queue, not single-PR latency.

---

**Files referenced**: `internal/orchestrator/orchestrator.go:106-150`, `orchestrator_config.go:32-73`, `orchestrator_schedule.go:32-128`, `orchestrator_prwatcher.go:23`, `adaptersync/adaptersync.go:122-200`, `adapter/githubissues/adapter.go:97`, `state/work_items_batch_upsert.go:28`, `scheduler/scheduler.go:296-410`, `scheduler/scheduler_spawn.go:21-130`, `spawner/claude.go:152-200`, `spawner/exit_reason.go:1-60`, `prwatch/prwatch.go:219-310`, `checks/poller.go:44`, `merge/coordinator.go:22-67`, `merge/worker.go:67-80`, `merge/prober.go:30-118`, `reaper/reaper.go:134-220`, `obs/events.go:21-152`, `web/dashboard.go:425-431`, GH issue #1093.

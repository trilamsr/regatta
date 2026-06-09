---
title: "Pause/resume CLI for in-flight agents (#1071)"
status: design
phase: self-host-s2
issue: 1071
summary: "Add three scriptable verbs — `regatta agents pause <ID>`, `regatta agents resume <ID>`, `regatta agents kill <ID> [--reason R]` — so an operator can park an in-flight agent without `sqlite3 ... UPDATE agents SET state='crashed'` + `kill -9 <pid>`. Pause is a soft side-table flag (no new agent-state column, no FSM edge); the running child runs to natural completion, but the scheduler skips its work item on every subsequent Tick until resume clears the flag. Kill is the existing `crashed` transition wired to a process signal. Exit codes are stable (0/2/1). Builds on the `dispatch_resume_intents` row pattern from the sibling provider-halt-gate spec (#1096) — same idempotent-CAS-on-NULL shape, scoped per-agent. No new substrate-event kind for pause/resume in v1; reuses the existing `agent.exited` for kill and a new lightweight `agent.paused`/`agent.resumed` audit pair."
---

# Pause/resume CLI for in-flight agents — Design Spec

Status: design
Date: 2026-06-09
Author: design subagent (session-1071)
Tracks: #1071 (this spec).
Cross-ref: `cmd/regatta/agents.go` (sibling read-only CLI shape — closes #1078), `internal/orchestrator/state/agents.go::TransitionAgent` (state-machine entry), `internal/orchestrator/state/transitions/tables.go::AgentEdges` (closed enum), `internal/orchestrator/state/work_items_query.go::ListSpawnable` (the loop that needs to learn the pause filter), `internal/orchestrator/scheduler/scheduler.go::Tick` (the consumer that re-reserves paused work items today), `docs/engineer/specs/2026-06-08-provider-halt-gate.md` §4.2 (`dispatch_resume_intents` row primitive — same shape reused at agent scope).

Memory rules in force: `feedback_default_simpler`, `feedback_no_signatures`, `feedback_cite_origin_main_not_local`, `feedback_spec_pattern_authority`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_adversarial_review_every_step`, `feedback_no_self_tagged_approve`, `feedback_validate_before_ship`.

---

## Problem

The 2026-06-08 dogfood session ended with the operator wrapping up while seven `claude` subprocesses were still mid-tool-call. To preserve worktrees but stop new work, the operator ran the moral equivalent of:

```
$ pgrep -af claude | awk '{print $1}' | xargs kill -9
$ sqlite3 regatta.db "UPDATE agents SET state='crashed' WHERE state IN ('spawning','running','pr_open','gates_running');"
```

Both halves are wrong. The `kill -9` skips graceful shutdown so partial commits in the worktree are lost. The `UPDATE` violates `transitions.AgentEdges` — `pr_open` cannot transition to `crashed` legally in some sub-states (verified at `internal/orchestrator/state/transitions/tables.go::AgentEdges` against `origin/main`: `git ls-tree origin/main:internal/orchestrator/state/transitions tables.go`). The substrate event log stays silent because no Go code path emitted the transition. Net effect: state DB and reality diverge, no audit trail, the next `regatta serve` boot picks the work items up again as spawnable because `ListSpawnable` only filters on `agents.id IS NULL` (origin/main `internal/orchestrator/state/work_items_query.go:65-67`), not on a `paused` flag.

Symptom inventory (counts verified against `origin/main` at HEAD `ca71046`):

- Verbs available on the CLI for in-flight agent control: zero. `cmd/regatta/agents.go` ships `list` only (`git ls-tree origin/main:cmd/regatta agents.go` → exists; `agentsSubList = "list"`; switch has one case).
- Scheduler bypass for "agent in-flight but operator-paused": none. `ListSpawnable` re-derives spawnability from `(status, agent.id IS NULL, deps, gates)`; no `paused`-aware predicate.
- Signal handling on `agents`: `cmd/regatta/agents.go` never imports `os/signal` or `syscall`. The spawner does (`internal/orchestrator/spawner/claude.go`) but only for the daemon's own shutdown — not per-agent.
- AgentEdges allowed terminal states: `done`, `withdrawn`, `crashed`, `escalated`. No `paused`. The issue body's "transitions DB state back to `running`" call would require a new `pending → running` edge or a `paused` state — both expand the closed-enum surface (`feedback_default_simpler` violation if avoidable).

Adversarial framing: the issue body proposes a new `paused` state in the FSM + a SIGSTOP-to-the-child wire. Both are over-engineered. SIGSTOP freezes the child in-place, holding open API requests, file handles, and an HTTPS keep-alive that Anthropic will time out at; on SIGCONT the child resumes in an undefined state. A `paused` AgentState adds a node + ~13 new edges to the closed `transitions.AgentEdges` enum: in-edges from every non-terminal state (`pending→paused`, `spawning→paused`, `running→paused`, `pr_open→paused`, `gates_running→paused`, `awaiting_merge→paused`), symmetric out-edges back to each originating state, and explicit `paused→withdrawn`, `paused→crashed`, `paused→done` terminations. Each new edge needs its own test in `internal/orchestrator/state/transitions/tables_test.go` and a wire in every consumer that switches on AgentState. Reviewer flagged the original "four edges" claim as off by ~69%; corrected here. Default-simpler is to leave the child alone and stop the *scheduler* from picking the work item up again. The currently-running spawn either completes (good — its PR lands like any other) or the operator kills it (covered by verb 3).

## Design

**Goal**: operator can answer "stop spawning new work for agent N, but let the in-flight subprocess finish on its own clock" with one scriptable command, and reverse it with one more. No SQL UPDATE, no kill -9, no FSM expansion.

Three verbs under the existing `agents` subcommand tree in `cmd/regatta/agents.go` — same `runAgentsList`-shaped dispatch, same `--db`/`REGATTA_DB` resolution, same exit-code grammar.

### Verb 1 — `regatta agents pause <ID> [--reason R]`

Inserts a row into a new `agent_pause_intents` table keyed by `agent_id`. The scheduler reads this table on every Tick and excludes any work item whose latest non-NULL-`cleared_at` row is currently active. Side-table, not a new column on `agents`.

- No FSM transition.
- No signal sent to the child process. The child keeps running until natural exit (PR opened → `pr_open` transitions on its own clock).
- One substrate event `KindAgentPaused` is appended for audit.
- Idempotent: a second `pause` while already paused inserts no duplicate row (`INSERT ... WHERE NOT EXISTS (SELECT 1 FROM agent_pause_intents WHERE agent_id=? AND cleared_at IS NULL)`); exits 0 either way with stdout `paused` (already-paused emits `already-paused`).
- `--reason R` is free-form text persisted into the row + event payload. Optional. Empty when omitted.

Effect on Tick: a new `applyAgentPauseFilter` step runs INSIDE the existing `ListSpawnable` consumer — after `db.ListSpawnable` returns the WorkItem set, the scheduler queries the active-pause set (`SELECT work_item_id FROM agents JOIN agent_pause_intents ON agents.id = agent_id WHERE cleared_at IS NULL`) and drops matches. Single index lookup per Tick; not in the SQL hot path.

Rationale for filter-in-Go rather than `ListSpawnable` SQL edit: keeps `work_items_query.go` byte-equal on this change (no `feedback_byte_equal_refactor_pin` debt — the existing benchmark pins the SQL). New table → new filter → new Go-level join. The next round of work-items query restructuring is free to fold the filter into SQL.

### Verb 2 — `regatta agents resume <ID>`

CAS-updates the active row in `agent_pause_intents` to set `cleared_at = strftime('%s','now')`. Exits 0 with stdout `resumed`. If no active row exists, exits 0 with stdout `not-paused` (idempotent — same shape as `pause` already-paused). Appends `KindAgentResumed` with payload `{cleared_by: "operator", paused_for_seconds: N, reason: <original-pause-reason>}`.

The scheduler's next Tick sees no active row, so the work item appears in `ListSpawnable`'s output and is re-eligible. If the original in-flight subprocess is still running (paused but never killed), the existing `ListSpawnable` `agents a ON w.id = a.work_item_id ... AND a.id IS NULL` filter already excludes it from re-spawn — covered by the join contract, no new code path.

### Verb 3 — `regatta agents kill <ID> [--reason R]`

Two-step, transactional in spirit:

1. Read the agent row; verify `state IN (spawning, running, pr_open, gates_running, awaiting_merge)` (the non-terminal in-flight set). Other states exit 2 with `agent N is in terminal state X; nothing to kill`.
2. Signal the child PID. Default `SIGTERM`; `--force` sends `SIGKILL`. POSIX-only via `syscall.Kill`; Windows build tag uses `os.Process.Kill()` fallback (matches `cmd/regatta/reload_secrets.go` precedent).
3. Drive a state-machine transition to `crashed` via `db.TransitionAgent(ctx, id, state.AgentCrashed, AgentMutation{})`. The reason text lands in a new substrate `KindAgentKilledByOperator` event payload.

Existing edges already permit `{spawning, running, pr_open, gates_running, awaiting_merge} → crashed` (verified against `internal/orchestrator/state/transitions/tables.go` at `origin/main`). No new edge. The signal-then-transition order is intentional: if the transition fails (e.g. concurrent FSM update), the operator can re-run `kill` against the now-orphaned process without double-transitioning.

### Schema: `agent_pause_intents`

New migration `NNN_agent_pause_intents.sql` (NNN pinned at dispatch time per `feedback_migration_number_lock`; current top of `internal/orchestrator/state/migrations/` is `0021`, so the implementer brief names `0022` provisionally — confirmed via `ls origin/main:internal/orchestrator/state/migrations`):

```sql
CREATE TABLE agent_pause_intents (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id     INTEGER NOT NULL REFERENCES agents(id),
  requested_at INTEGER NOT NULL,
  reason       TEXT,
  cleared_at   INTEGER,
  cleared_by   TEXT
);
CREATE UNIQUE INDEX idx_agent_pause_active
  ON agent_pause_intents(agent_id) WHERE cleared_at IS NULL;
```

The partial unique index on `(agent_id)` where `cleared_at IS NULL` is the idempotence guarantee — a second `pause` while paused fails on the unique constraint, which the CLI catches and converts into `already-paused` stdout + exit 0. No race window.

Why a side table not a column on `agents`:

- Keeps `transitions.AgentEdges` byte-equal — closed-enum surface unchanged, no FSM debt.
- Keeps an audit trail (one row per pause/resume cycle).
- Survives a `regatta dispatch resume` (provider-halt scope) untouched — distinct scopes, distinct tables.
- Pattern-symmetric with `dispatch_resume_intents` (sibling spec §4.2) — both are operator-issued intents the scheduler polls on next Tick.

### Substrate event kinds

Two new kinds in `internal/orchestrator/state/substrate/event.go`:

- `KindAgentPaused` — payload `{agent_id, requested_at, reason}`. Emitted once per pause transition (not per blocked Tick — the scheduler MUST NOT re-emit; the active row IS the state).
- `KindAgentResumed` — payload `{agent_id, paused_for_seconds, cleared_by, original_reason}`.

`KindAgentKilledByOperator` is also new; it's distinct from the existing `agent.exited`/`agent.crashed` events because the cause is operator-issued, not provider/spawner. Lets the dashboard / event tail distinguish "operator parked this" from "subprocess died of its own accord". Payload: `{agent_id, signal: "SIGTERM"|"SIGKILL", reason}`.

No new event kind for `pause`-skipped-Ticks. The scheduler emits a `slog.Debug("scheduler.agent_paused_skip", agent_id, work_item_id)` only — debug-level, not substrate. Operator UX via `regatta agents list` + a new `paused_at` column on the list output (NULL when not paused).

### Decision priority

UX > ease > performance > best-practices. The simplest verb set the operator can script in `bash` covers ≥95% of the issue's stated symptoms (manual SQL + kill -9). The Phase-X polish (SIGSTOP-the-child, carry-forward-fingerprint resume) is deferred — see Out of scope.

## Acceptance

- **c1** — Given a `running` agent N with PID P, when the operator runs `regatta agents pause N --reason "operator EOD"`, exit code is 0, stdout is `paused`, a row in `agent_pause_intents` exists with `agent_id=N, cleared_at IS NULL, reason="operator EOD"`, a `KindAgentPaused` substrate event is appended, and the child process at PID P is still running with the same PID (no signal sent).
- **c2** — Given the c1 paused state, when the operator runs `regatta agents pause N` again, exit code is 0, stdout is `already-paused`, and no duplicate row is inserted (verified by `SELECT COUNT(*) FROM agent_pause_intents WHERE agent_id=N AND cleared_at IS NULL = 1`).
- **c3** — Given the c1 paused state, when the scheduler runs Tick, then for any WorkItem whose `agent_id = N`, the WorkItem is dropped from the spawnable set BEFORE the L0/cost/L4 gate chain runs (covered by a unit test asserting the filter ordering).
- **c4** — Given the c1 paused state, when the operator runs `regatta agents resume N`, exit code is 0, stdout is `resumed`, the `agent_pause_intents` row has `cleared_at` set to a UTC unix timestamp within 5s of `now`, and a `KindAgentResumed` substrate event is appended.
- **c5** — Given a never-paused agent N, when the operator runs `regatta agents resume N`, exit code is 0, stdout is `not-paused`, and no substrate event is emitted.
- **c6** — Given a `running` agent N with PID P, when the operator runs `regatta agents kill N --reason "shutting down"`, exit code is 0, stdout is `killed`, the child at PID P receives `SIGTERM` (verified by `syscall.Kill(P, 0)` returning ESRCH within `testutil.Eventually` 5s budget), the `agents.state` column is `crashed`, and a `KindAgentKilledByOperator` event with `signal="SIGTERM", reason="shutting down"` is appended.
- **c7** — Given a `done` agent N, when the operator runs `regatta agents kill N`, exit code is 2 with stderr `agent 5 is in terminal state done; nothing to kill`, no signal is sent, no state transition, no event.
- **c8** — Given a non-existent agent ID 999, all three verbs (`pause`, `resume`, `kill`) exit 2 with stderr `agent 999 not found` (exit 2 = "operator-input wrong"; exit 1 = "operation failed mid-flight" — convention from `cmd/regatta/agents.go::runAgentsList`).
- **c9** — `regatta agents list` includes a `PAUSED_AT` column populated from the latest active `agent_pause_intents.requested_at` (NULL when not paused). JSON output adds a `paused_at` field with the same NULL semantics.
- **c10** — All three verbs treat missing `<ID>` as exit 2 with usage stderr; non-integer `<ID>` as exit 2. Matches the precedent from `cmd/regatta/agents.go::runAgents` lines 23-34.
- **c11** — TDD-order verification: `git log --reverse <branch> -- cmd/regatta/pause_test.go cmd/regatta/resume_test.go cmd/regatta/kill_test.go` MUST show RED-commit-first GREEN-second. PR body MUST include the RED `go test` output.

## Out of scope

- **SIGSTOP-the-child pause** — the issue body proposes `kill -STOP <pid>`. Rejected for v1: a stopped child holds open HTTPS keep-alives Anthropic will close (resulting in a broken request on resume), holds file locks in the worktree (blocking the operator from inspecting), and offers no UX win over letting the in-flight run finish on its own. Reopen when an empirical dogfood case shows ≥3 sessions where a long-running tool-call needs to be frozen mid-flight rather than allowed to complete.
- **`pause --all`** — issue body suggests it. Defer; the operator's documented session-end workflow needs per-agent reason logging more than a sweep. `for id in $(regatta agents list --state running --format json | jq '.[].id'); do regatta agents pause $id --reason wrapping; done` covers the use case in two lines of bash. Reopen if the operator runs the loop ≥3x per week.
- **`resume` carries forward last-commit fingerprint** — issue body c2. Out of scope here; that's a *re-spawn* primitive, which is a separate feature (the agent never died, just got skipped). Reopen when verb 1 is rewired to actually halt the child (see SIGSTOP above) — fingerprint becomes meaningful only then.
- **New `paused` AgentState column** — explicitly rejected; see Design rationale.
- **CLI shell completion** — Phase-X polish.
- **gRPC / HTTP API surface for pause/resume** — self-host phase is CLI-only per `feedback_decision_priority`.
- **Multi-daemon pause coordination** — single-operator-single-daemon norm.
- **Pause TTL / auto-resume** — sibling provider-halt-gate has `auto_release_after`; agent-pause is operator-only release in v1. Reopen if the operator forgets a pause >24h and complains.
- **`kill --force` default to SIGKILL** — explicit flag required; default is SIGTERM (graceful) to preserve worktree integrity.

## Adversarial

Independent reviewer (cavecrew-reviewer or equivalent) MUST be spawned BEFORE any APPROVE token lands on an implementer PR. The reviewer-verdict gate (`scripts/check-reviewer-verdict.sh`) covers `docs/engineer/specs/*.md` as a load-bearing surface — this spec's own PR requires an independent adversarial pass before merge per `feedback_adversarial_review_every_step` + `feedback_no_self_tagged_approve`.

Hunt for:

- **Race between pause + a tick already in-flight**: operator runs `pause N` while the scheduler is mid-Tick and has already pulled work item W from `ListSpawnable` but not yet reserved it. The `pause` writes the row, the Tick commits the reservation — the work item is reserved AGAINST a now-paused agent. Mitigation: the pause-filter step runs immediately after `ListSpawnable` AND again in the reservation closure (via `WithTx`-aware check `SELECT 1 FROM agent_pause_intents WHERE agent_id=? AND cleared_at IS NULL` joined into the existing `UpsertPendingTx` path). Both checks are required because Tick is not transactional across ListSpawnable + reserve. Failing test: `TestTick_PauseRaceMidReserve` — pause-row insert happens between the ListSpawnable call and the reserve closure; the reserve MUST abort with a typed error the Tick swallows (no error log spam — paused is a steady-state condition).
- **Resume-during-provider-halt-cooldown**: operator runs `regatta agents resume N` while `regatta dispatch resume` (sibling spec) is also pending. The two intents are independent — agent-pause is a per-agent filter; dispatch-halt is a process-wide gate. The Tick must respect BOTH: dispatch-halt short-circuits BEFORE any per-agent work happens, so a resumed agent N still doesn't get re-Ticked until dispatch is also resumed. **Ordering enforcement**: provider-halt spec (`docs/engineer/specs/2026-06-08-provider-halt-gate.md` §3.1) inserts `applyProviderHalt` BEFORE `gate_l0`. This spec inserts `applyAgentPauseFilter` between `gate_l0` and `gate_cost`. Concrete insertion point: `internal/orchestrator/scheduler/scheduler.go::Tick` step list — `applyProviderHalt` at position 0, `gate_l0` at position 1, `applyAgentPauseFilter` at position 2. Implementer MUST verify against whatever provider-halt actually ships (provider-halt spec is still status: design at write time); rebase to match if positions differ. Failing test: `TestTick_PausedAgentResumedDuringHalt_StaysBlocked`.
- **`kill` against an agent whose PID has been recycled**: process exited, OS reassigned PID to an unrelated `ps`. `syscall.Kill(pid, signal)` would SIGTERM a random innocent process. Mitigation: before signaling, read `/proc/<pid>/comm` (Linux) or `ps -p <pid> -o comm=` (macOS) and verify the substring is `claude` or matches the spawner-emitted process-binary name. On mismatch, exit 1 with `agent N pid P has been recycled by another process; refusing to signal`. Linux-only `/proc` lookup; macOS path uses a small `os/exec` wrapper. Adversarial: this check itself races (TOCTTOU). Accept the race — the window is sub-millisecond and the FSM transition still drives `crashed`, so worst case is a Bad Day stale signal. Document the residual race.
- **Sibling issue search**: `gh issue list --search "pause resume agent" --state all` returns ONLY #1071 (verified at session-1071 13:48 PT). No duplicates.
- **English error-message overlap with provider-halt**: pause/resume verbs operate on `agents`, halt on `dispatch`. CLI verb namespace is disjoint (`agents pause` vs `dispatch resume`). Operator help text MUST cross-reference both to avoid the operator running `regatta agents resume` expecting a dispatch-level effect.
- **`pause` while agent is `pr_open`**: the work item is no longer in `ListSpawnable`'s output (the `agents a ON w.id = a.work_item_id` join excludes it via the `IS NULL` filter). Pausing a `pr_open` agent is a no-op for spawnability but still records the intent. Test asserts no-op semantics + audit row.
- **`kill` after the agent has organically transitioned to `done`**: c7 covers terminal states. Edge case: the transition happens BETWEEN the CLI's state read and its `TransitionAgent` call. The transition fails with `ErrInvalidTransition: done → crashed`; CLI exits 2 with stderr noting the race (matches c7 bucket).
- **`kill` assumes daemon + child on same host/OS**: `syscall.Kill(pid, signal)` only signals processes inside the same kernel. CLI run from a workstation against a daemon on a remote host (or different docker namespace) either signals the wrong process or fails with EPERM. v1 explicitly requires the CLI to run on the same host as the daemon — `--help` output states it. Reopen when multi-daemon coordination becomes a use case (per §11 Out of scope).

Survey-level pass per `feedback_subagent_survey_adversarial_pass`: this spec drew on one upstream survey (the provider-halt sibling spec). The single input is reviewed at the brief level (this spec's design lines cite line-numbered claims against `origin/main`).

## Implementer brief

Slug: `agents-pause-resume-cli`
Branch: `fix/1071-agents-pause-resume-cli`
Migration N: pinned at dispatch time; current head is `0021_substrate_kind_tool_call.sql`. Provisionally `0022_agent_pause_intents.sql` — implementer MUST confirm by re-running `ls internal/orchestrator/state/migrations/ | sort | tail -1` against `origin/main` at the dispatch moment.
File scope (additive, no deletes — defense for `feedback_deletion_default` lives in the PR body):

- ADD `cmd/regatta/pause.go` — `runAgentsPause(args []string) int`.
- ADD `cmd/regatta/resume.go` — `runAgentsResume(args []string) int`.
- ADD `cmd/regatta/kill.go` — `runAgentsKill(args []string) int`.
- ADD `cmd/regatta/{pause,resume,kill}_test.go` — RED commits first per `feedback_tdd_discipline`.
- EDIT `cmd/regatta/agents.go::runAgents` — extend the switch with three new cases (3-line edit; preserve existing list dispatch byte-equal).
- EDIT `cmd/regatta/agents.go::emitAgentsTable` + `emitAgentsJSON` — add `PAUSED_AT`/`paused_at` column per c9.
- ADD `internal/orchestrator/state/agent_pause_intents.go` — `Pause/Resume/IsPaused/ListActivePauses` methods on `*DB`.
- ADD `internal/orchestrator/state/agent_pause_intents_test.go` — partial-index uniqueness + idempotence + tx-aware variants.
- ADD `internal/orchestrator/state/migrations/0022_agent_pause_intents.sql` (NNN provisional).
- ADD `internal/orchestrator/state/substrate/event.go` kinds — `KindAgentPaused`, `KindAgentResumed`, `KindAgentKilledByOperator`.
- ADD `internal/orchestrator/scheduler/scheduler_agent_pause_filter.go` — `applyAgentPauseFilter(ctx, spawnable) ([]state.WorkItem, error)`.
- ADD `internal/orchestrator/scheduler/scheduler_agent_pause_filter_test.go`.
- EDIT `internal/orchestrator/scheduler/scheduler.go::Tick` — insert `applyAgentPauseFilter` after `ListSpawnable`, before the cost-cap step. One new line + ordering test.
- EDIT `internal/orchestrator/state/agents.go::UpsertPendingTx` — add the pause-check guard inside the reservation closure (covers the mid-Tick race in Adversarial). Declare a typed sentinel `var ErrAgentPaused = errors.New("agents: paused")` at package level so the scheduler can `errors.Is(...)`-check and quietly skip without error-log noise. `ErrAgentPaused` does NOT exist on origin/main today; implementer adds it. Missing this declaration would crash Tick error-handling with a nil-comparison fallthrough.
- ADD `docs/engineer/specs/2026-06-09-pause-resume-cli.md` — this spec (lands in the design PR).

Spec pattern authority per `feedback_spec_pattern_authority`: implementer deviation from the side-table-not-column choice MUST re-spawn the designer subagent. Do not let the implementer add a `paused` AgentState; that defeats the entire `feedback_default_simpler` rationale.

Independent reviewer required (load-bearing — agents lifecycle + scheduler hot path + new CLI verbs). Per `feedback_no_self_tagged_approve`: spawn fresh-slot reviewer BEFORE the APPROVE token lands. `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` in PR body footer (bare, not in a code block).

No automerge from implementer; `gh pr ready <N>` only. Per `feedback_no_implementer_automerge`.

Comment budget per `feedback_comment_budget_enforcement`: WHY-not-WHAT default-zero. Godocs on new exported methods MUST capture WHY in one sentence. Test/Fuzz/Benchmark godocs ≤1 line.

Test plan:
- Unit: pause/resume idempotence + partial-index uniqueness; agent_pause_intents CAS; scheduler filter ordering; signal verification with a fake `syscall.Kill`; PID-recycle guard.
- Integration: `TestTick_PauseRaceMidReserve`, `TestTick_PausedAgentResumedDuringHalt_StaysBlocked`, `TestTick_PauseSkipsBeforeGates`.
- E2E (`cmd/regatta/pause_test.go` family): golden table output, exit-code matrix (0/1/2 per c1-c10), POSIX-only `syscall.Kill(0, pid)` post-kill check.
- Property: `FuzzPauseResumeRoundtrip` — random pause/resume/kill sequences; invariant: `agent_pause_intents` never has >1 active row per agent_id.
- Manual: 3-agent dogfood scenario from `cmd/regatta/serve_claude_test.go`-style shim — pause, verify next Tick skips, resume, verify next Tick proceeds, kill, verify FSM lands `crashed`.

## Reopen trigger

Reopen this spec when ANY of:

- Operator sessions show ≥3 cases of `kill -STOP` needed mid-flight rather than allowing natural completion (re-evaluate SIGSTOP-the-child rejection in Out of scope).
- The `pause --all` sweep gets requested ≥3 sessions per week (re-evaluate Out of scope).
- A second agent runtime (BYOM — different spawner binary, e.g. `gemini-cli` or `aider`) lands and the PID-recycle guard's `claude`-substring check needs parameterization.
- The pause TTL / auto-resume feature gets requested after an operator forgets a pause >24h.
- Resume-while-already-running races trigger a new bug class — current design assumes the child either completes naturally or is killed; a half-paused-half-running state would invalidate the side-table-only approach.
- A new sibling spec adds a process-wide "halt all agents" verb that overlaps with `pause --all` — fold the two before shipping either.

# Orchestrator PR Watcher — Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent <tri@lumalabs.ai>
Supersedes: GH issue #15 (filed 2026-05-21 against an earlier `internal/orchestrator/prwatcher` shape; the orchestrator now drives state via `PollOnce` / `ScheduleOnce` / `ReapTerminal`, so the older package-as-noun framing no longer fits.)

Downstream seams (separate work items, do not bundle):

- Issue #33 — L3/L4/L5 gate runners. This spec stops at emitting an `agent_pr_opened` substrate event keyed by `(agent_id, pr_sha)`; the gate runner is whatever consumes that event.
- Issue #16 — RejectionRouter. Same substrate-event seam; out of scope here.

Memory rules in force: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_test_godoc_one_line`, `feedback_deletion_default`, `feedback_doc_check_banned_phrases`, `feedback_pr_body_release_notes_fence`, `feedback_pr_body_file_only`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`.

---

## §1 Goal + non-goal

### 1.1 Goal

Drive the `running → pr_open` agent transition by polling GitHub for the head SHA of the branch each running agent owns. Persist the SHA via `state.AgentMutation.PRSHA`. Re-emit a substrate event whenever the SHA changes so downstream consumers (gate runner #33, rejection router #16) idempotently key on `(agent_id, pr_sha)`.

Concretely, after this spec lands:

- Every tick scans agents in `running` and `pr_open` (the rewake set).
- For each one whose branch `regatta/agent-{id}` has an open PR, the orchestrator records `pr_sha`, transitions `running → pr_open` on first sighting, and records `pr_head_changed` events on subsequent head pushes.
- On `PR closed` (merged or otherwise without merge), no transition fires here — the closed-PR path stays with the reaper / merge-watch flow (out of scope; ship after #33).

### 1.2 Non-goal

- Not running gates. The substrate event is the seam; the gate executor lives behind #33.
- Not transitioning `pr_open → gates_running`. That edge fires when the gate runner reserves a (pr_sha, gate_id) tuple; this spec only emits the event.
- Not transitioning `awaiting_merge → done`. Merge detection is its own watch (filed at PR time per `feedback_unaddressed_load_bearing`); the close-without-merge path is downstream.
- Not webhook-driven. Polling is sufficient at single-operator scale; webhook receivers are a Phase X concern (multi-tenant, public ingress).
- Not a new long-lived goroutine. The watch runs inside the existing tick driver.

---

## §2 In / Out

### IN

- One new package `internal/orchestrator/prwatch` exposing `Watcher` with a single method `Sweep(ctx) error`.
- One new field on `orchestrator.Orchestrator`: `prwatch *prwatch.Watcher`, wired via `SetPRWatcher` mirroring `SetReaper` so the daemon stays optional-deps-friendly.
- One new tick-loop case in `orchestrator.Run`: `prwatch.Sweep` runs on the existing `tickT` ticker alongside `ReapTerminal`, so cadence + cancellation are inherited.
- One new GitHub seam `prwatch.PRLister` interface: `ListOpenByHead(ctx, owner, repo, head) ([]PullRequest, error)` — production impl shells `gh pr list --json number,headRefOid,state --head <branch>`; tests inject a stub.
- One new substrate event kind `agent_pr_head_changed` with payload `{agent_id, pr_number, pr_sha, prev_sha}`.

### OUT

- Webhook receiver. Polling on the existing 5s tick is good enough at one-repo, one-operator scale (5s × 200 agents = 40 gh calls/s worst case; gh CLI auths via the operator's local token, no rate-limit headache surfaces below ~5k req/h).
- `go-github` SDK dependency. The gh CLI is already a hard prereq for spawner output (`gh issue comment`, etc.) and is already authenticated in operator's shell; adding a Go SDK is a duplicate auth seam.
- The merge watch (`awaiting_merge → done`). Filed as a sibling spec when #33 lands.
- Tracking PR comments / reviews. Gate verdicts are written by the gate runner (#33); this watcher only sees the head SHA.

---

## §3 Architecture

### 3.1 Prior art adopted (≥2 OSS, per `feedback_research_design_principles`)

| Pattern | Source | What we adopt |
|---|---|---|
| Tick-driven external state polling | Kubernetes controller-runtime reconcilers — every controller is `Reconcile(ctx, req)` driven by a workqueue, no per-resource goroutines | One tick method (`Sweep`) over the agent list, no per-agent goroutines, idempotent by design. |
| External CLI shell-out as the auth seam | `gh` CLI itself + Tekton's `git-clone` task (shells `git` rather than re-implementing the protocol) | Reuse `gh pr list --json` for PR lookup; rely on the operator's existing `gh auth status`. Zero new credential surface. |
| Idempotent state key = `(resource, observed_state_version)` | Argo CD's `(application, syncRevision)` keying | Our key is `(agent_id, pr_sha)`. Repeat events for the same pair are no-ops; SHA change triggers exactly one new event. |
| `go-github` direct SDK | google/go-github | Considered. Rejected — duplicates the gh-CLI auth path already wired into operator shells and the spawner, adds a vendored dep, and forces us to own token refresh. The gh CLI handles all three. |

Adoption-first holds: the new code is `Sweep`'s loop + the SHA-diff fold; the polling pattern, the CLI shell-out, and the (id, version) keying are all borrowed.

### 3.2 Correlation primitive — branch name as the agent ↔ PR key

`spawner.WorktreeManager.BranchFor(agentID)` already returns `regatta/agent-{id}` deterministically. The watcher uses this branch name as the GitHub query key:

```
gh pr list --head regatta/agent-7 --state open --json number,headRefOid,state
```

No new column on `agents` is needed. The `pr_sha` column already exists (state migration 0001) and `AgentMutation.PRSHA` is already plumbed through `TransitionAgent` — verified by `grep -n PRSHA internal/orchestrator/state/agents.go` (lines 19, 74, 105, 146, 161, 265, 273, 275, 283, 299).

### 3.3 Watcher contract

```go
// internal/orchestrator/prwatch/prwatch.go (new package, ~120 LoC)
package prwatch

type PullRequest struct {
    Number     int
    HeadRefOid string
    State      string // "OPEN" | "CLOSED" | "MERGED"
}

// PRLister is the GitHub seam. Production impl shells gh CLI; tests stub.
type PRLister interface {
    ListOpenByHead(ctx context.Context, branch string) ([]PullRequest, error)
}

type Watcher struct {
    db       *state.DB
    branchFn func(agentID int64) string // injected from spawner.WorktreeManager.BranchFor
    lister   PRLister
    log      *slog.Logger
    tracer   trace.Tracer
}

// Sweep walks agents in {running, pr_open} and reconciles their pr_sha
// against GitHub. One tick. Idempotent. Errors per-agent are logged
// and skipped — a single agent's network blip must not abort the sweep.
func (w *Watcher) Sweep(ctx context.Context) error
```

`Sweep`'s decision matrix per agent:

| Agent state | gh result | Action |
|---|---|---|
| `running` | no open PR | no-op (PR not opened yet) |
| `running` | 1 open PR, sha = X | `TransitionAgent(running → pr_open, PRSHA: X)` + emit `agent_pr_opened` |
| `running` | >1 open PR | log warn `pr_watcher.ambiguous_head`, pick lowest `number`, transition |
| `pr_open` | no open PR | no-op (close path; downstream `reaper.PRClosed` filed separately) |
| `pr_open` | open PR, sha unchanged | no-op |
| `pr_open` | open PR, sha changed | `UPDATE agents SET pr_sha=X` (no transition) + emit `agent_pr_head_changed` |

The `pr_open → gates_running` transition stays with the gate runner (#33). The watcher's job ends at "record the SHA + emit the event."

### 3.4 New substrate event kinds

Two kinds, both keyed by `(agent_id, pr_sha)` so downstream consumers fold by tuple:

- `agent_pr_opened` — fires on `running → pr_open`. Payload `{pr_number, pr_sha}`.
- `agent_pr_head_changed` — fires on any subsequent SHA change while in `pr_open`. Payload `{pr_number, pr_sha, prev_sha}`.

Both write via `db.RecordEvent(ctx, agentID, kind, jsonPayload)` — the existing substrate-event seam used by `spawned`, `reaped`, etc. No schema migration; the `events` table already accepts arbitrary `kind` strings.

### 3.5 Auth + rate-limit posture

- Auth: `gh` CLI uses the operator's `~/.config/gh/hosts.yml` token. The watcher inherits that — no new env vars, no new secret.
- Rate limit: GitHub REST allows 5 000 req/hr for an authenticated user. At 5 s tick × 1 req/agent, one agent burns 720 req/hr. Practical ceiling ≈ 6 concurrent agents before headroom matters. The watcher logs `pr_watcher.rate_warn` when `gh` exits with a 403/429 sentinel; the sweep skips the rest of the tick and resumes next cadence.
- The downstream "use webhooks" upgrade path is Phase X (multi-tenant) and is filed as a follow-up at PR time, not in this spec.

### 3.6 Production wiring

`cmd/regatta/serve.go::buildOrchestrator` (existing) gains a `prwatch.New(...)` construction line + an `orch.SetPRWatcher(w)` call, mirroring how the reaper wires today. The PRLister field defaults to a `gh`-CLI-shelling impl; `--no-pr-watch` flag (added to the same `serve` cobra command) wires nil for smoke-test fixtures that don't have a GitHub remote.

The `Run` loop adds one branch alongside `ReapTerminal`:

```go
case <-tickT.C:
    if err := o.ScheduleOnce(ctx); err != nil { … }
    if err := o.ReapTerminal(ctx); err != nil { … }
    if err := o.WatchPRs(ctx); err != nil {   // new
        o.log.Warn("orchestrator.prwatch_failed", string(obs.KeyErr), err.Error())
    }
```

`o.WatchPRs` is the orchestrator-side adapter that delegates to `o.prwatch.Sweep` if `o.prwatch != nil`. Same nil-safety shape as `ReapTerminal`.

### 3.7 File-disjoint task breakdown (one PR, three commits)

| Task | File | Scope |
|---|---|---|
| T1 — package + types + sweep loop | `internal/orchestrator/prwatch/prwatch.go` (new), `internal/orchestrator/prwatch/prwatch_test.go` (new) | The `Watcher` struct, `Sweep` method, in-memory PRLister stub, table-driven test of the decision matrix in §3.3. ~250 LoC including tests. |
| T2 — gh-CLI lister impl | `internal/orchestrator/prwatch/ghcli.go` (new), `internal/orchestrator/prwatch/ghcli_test.go` (new) | `ghCLILister` shelling `gh pr list --json`. Test via `httptest` mock GH API + `GH_HOST` override per the gh CLI docs at <https://cli.github.com/manual/gh_help_environment>. ~120 LoC. |
| T3 — orchestrator wiring | `internal/orchestrator/orchestrator.go` (existing), `cmd/regatta/serve.go` (existing) | `SetPRWatcher`, `WatchPRs` method, `Run`-loop case, `--no-pr-watch` cobra flag. Update package godoc to drop "PRWatcher … land in follow-up commits" line at L12. ≤40 LoC delta. |

T1 + T2 are file-disjoint and could parallelize; T3 depends on T1's exported surface. Rebase order T1 → T2 → T3 in one PR.

---

## §4 Risk register

### R1 — `gh pr list --head` returns multiple PRs for the same branch

A force-push that closes one PR and opens another can briefly list both. Picking randomly produces flapping `pr_sha` writes.

**Mitigation**: §3.3 row 3 — pick the lowest PR number deterministically. The watcher logs `pr_watcher.ambiguous_head` so the operator notices; no auto-resolution attempt. T1 includes a 2-PR fixture case in the decision-matrix test.

### R2 — gh CLI missing or unauthenticated on the operator machine

The daemon starts but every sweep fails with `gh: command not found` or `gh auth status: not logged in`.

**Mitigation**: T3 adds a startup probe — `cmd/regatta/serve.go` runs `gh auth status` once at boot when `--no-pr-watch` is unset and fails fast with a one-line operator-actionable message. The probe is a Go `exec.LookPath("gh")` plus a 5 s `gh auth status` call; if either fails, serve exits non-zero with `error: gh CLI unauthenticated; run 'gh auth login' or pass --no-pr-watch`.

### R3 — Sweep latency unbounded by agent count

At N=200 agents the sweep makes 200 gh calls per tick. At 5 s tick that's ~40 req/s and likely past the GH rate ceiling.

**Mitigation**: T1's `Sweep` runs sequentially with a per-call 2 s timeout (`context.WithTimeout`). At N > 20 the watcher emits `pr_watcher.tick_slow` once per tick; at N > 50 the watcher self-throttles by skipping every other tick (one-line counter mod 2). The "real" fix — webhook — is filed as a followup at PR time per `feedback_unaddressed_load_bearing`.

### R4 — gh CLI output schema drift across gh versions

`gh pr list --json` field names are not API-versioned. A future gh release could rename `headRefOid` → `headOid`.

**Mitigation**: T2 pins the queried fields as constants in `ghcli.go` and parses via `encoding/json` with explicit struct tags. A schema-drift case fails the unit test before it ships. T2 also logs the gh version once at watcher construction so post-mortem investigations can correlate.

### R5 — Race between `Sweep` and `ScheduleOnce` mutating the same agent row

Both methods run on the same tick. `ScheduleOnce` may transition `pending → spawning → running`; the same tick's `Sweep` then sees the just-`running` agent and queries gh.

**Mitigation**: `TransitionAgent` is row-locked via `TransitionAgentTx` (existing). The pathological case is harmless — Sweep sees the new `running` row, queries gh (no PR exists yet because the agent just spawned), no-ops per §3.3 row 1. The opposite order is identical. No new locking required.

### R6 — `agent_pr_head_changed` event flood on a force-push storm

An agent doing `git rebase -i` locally and force-pushing 10 times in a minute writes 10 substrate events.

**Mitigation**: out-of-scope. The events table is append-only by design — substrate replay is unaffected. If event volume becomes a downstream-consumer (gate runner) cost driver, that's a #33 concern. Filed as a followup line at PR time, not addressed here.

### R7 — `--no-pr-watch` flag silently disables a load-bearing component in prod

Operator sets `--no-pr-watch` during a smoke test, forgets to unset it, ships to prod.

**Mitigation**: T3 wires the flag through `cmd/regatta/serve.go` with `cobra.Command.PersistentFlags()` and a `Hidden: false` + a help string starting `[smoke-test only]`. The startup log line is `orchestrator.starting pr_watch_enabled=false` so the operator sees it on every boot.

---

## §5 Grade rubric

### B (floor — ships)

- T1 + T3 land. The 4 happy-path rows of §3.3's decision matrix have a table-driven unit test that fails-loud if the transition is wrong.
- The `agent_pr_opened` event lands in `events` table on first PR open.
- `running → pr_open` transition fires once per agent and stays put across repeat sweeps.
- PR body includes a ```release-notes``` fence and the A+ scorecard.
- One-line godoc per `feedback_test_godoc_one_line` on every `Test*` / `Fuzz*`.

### A (target — expected)

- All B items.
- T2 lands: `ghcli.go` shells `gh pr list --json` and is unit-tested against an `httptest`-backed mock GH API (the gh CLI honors `GH_HOST` for routing).
- `--no-pr-watch` cobra flag + `gh auth status` startup probe (R2).
- `agent_pr_head_changed` fires on SHA change exactly once; repeat sweeps with the same SHA are silent (R6 noted, not fixed).
- Package-level godoc on `internal/orchestrator/prwatch/prwatch.go` cites this spec by path so future readers find the design.

### A+ (stretch — exceptional)

- All A items.
- The watcher's per-tick latency emits an OTel histogram `prwatch.sweep.duration_ms` per W6 spec §3.5, with one span `prwatch.sweep` covering the per-tick fan-out + one child span per gh call so traces show which agent is slow.
- R3 self-throttle is wired (skip-every-other-tick at N > 50 agents) with a `pr_watcher.tick_slow` slog line at N > 20.
- Webhook-receiver followup filed at PR time per `feedback_unaddressed_load_bearing` with a one-line title and a pointer to this spec's §3.5.
- The decision-matrix test is parametrized so a future row (e.g. "closed without merge → reaper-kick") slots in without rewriting fixtures.

---

## §6 Sequencing

- **Pre**: none. The state-machine edges (`running → pr_open`), the `PRSHA` column, and the substrate event seam all exist today.
- **Dispatch**: T1 → T2 → T3 in three commits on one branch; one PR with `[FEAT]` title prefix.
- **Post**: closes #15. Downstream gate runner (#33) reads `agent_pr_opened` events to schedule gate runs.

No spec dependency follows this — gate-runner spec (#33) consumes the substrate event seam and is independently designed.

---

## §7 Deferred (named-but-not-shipped per `feedback_unaddressed_load_bearing`)

- Webhook-driven PR head SHA detection (vs polling). Phase X — needs public ingress + signature verification. Filed as followup at PR time.
- `awaiting_merge → done` merge-watch. Sibling spec; ships alongside or after gate runner (#33).
- `pr_open → withdrawn` close-without-merge detection. Same sibling spec.
- `go-github` SDK migration. Triggered only if gh CLI auth path becomes a constraint (hosted backend / multi-tenant). Followup line at PR time.
- Per-agent rate-limit budget. Out of scope until N > 50 agents is a real load profile.

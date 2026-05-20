# Review 04 — Fleet Orchestrator Skeleton

Concrete file layout, types, state machines, persistence, and bootstrap
path for `tools/fleet/`. Conventions follow `STYLE.md` (slog, sentinel
errors, no `init()` side effects, kingpin CLI, `gopkg.in/yaml.v3`,
errgroup, OTel-style `Start/Shutdown`) and `PRINCIPLES.md` (trust under
load, one mechanism over many, defaults bias toward private).

## 1. File layout

```
tools/fleet/
├── README.md                       purpose, build, run, stability=development
├── example_config.yaml             minimum working YAML (one lane, dryrun)
├── doc.go                          package doc + SPDX header
├── config.go                       Config struct + Validate(); yaml.v3
├── config_test.go                  table-driven: parse, defaults, validation
├── factory.go                      New(cfg, logger) -> *Orchestrator
├── orchestrator.go                 daemon: composes scheduler+watchers+gates+state
├── orchestrator_test.go            wires fakes; lifecycle Start/Shutdown
├── scheduler.go                    lane scheduler; dep-graph resolution
├── scheduler_test.go               eligibility, lane caps, priority order
├── specwatcher.go                  MILESTONES.md parser -> WorkItems
├── specwatcher_test.go             golden-file parses; rubric prefix detection
├── agent.go                        spawn/reap; worktree + claude CLI process mgmt
├── agent_test.go                   fake-claude binary; PID file; reap on exit
├── prwatcher.go                    GitHub poll loop; emits PR events to channel
├── prwatcher_test.go               mock gh client; debounce; etag handling
├── state.go                        durable state: sqlite repository
├── state_test.go                   round-trip; crash-recovery; schema migration
├── lessons.go                      learn-from-mistakes capture -> draft PRs
├── lessons_test.go                 friction signal classification
├── errors.go                       sentinel errors (ErrAlreadyStarted, …)
├── github/
│   ├── client.go                   thin wrapper over `gh` CLI (exec)
│   ├── client_test.go              fake gh; rate-limit backoff
│   └── types.go                    PR, Comment, Review, Check
├── gates/
│   ├── gates.go                    Gate interface + Result type
│   ├── gates_test.go               interface contract test
│   ├── l3_rubric.go                rubric verifier (Opus 4.7)
│   ├── l3_rubric_test.go           fixture PR -> deterministic verdict (mock)
│   ├── l4_adversarial.go           adversarial reviewer (Opus 4.7)
│   ├── l4_adversarial_test.go
│   ├── l5_drift.go                 drift detector (Sonnet 4.6)
│   ├── l5_drift_test.go
│   └── claude/                     shared claude-subagent invoker
│       ├── invoker.go              `claude --print --output-format json`
│       └── invoker_test.go         fake-claude; token accounting
├── prompts/
│   ├── milestone.tmpl              spawn prompt: milestone block + iron law
│   ├── l3_rubric.tmpl
│   ├── l4_adversarial.tmpl
│   └── l5_drift.tmpl
├── testdata/
│   ├── milestones_basic.md         small MILESTONES.md fixture
│   ├── pr_clean.json               gh pr view --json fixture (passes)
│   └── pr_broken.json              fixture missing rubric citation
└── cmd/
    └── fleet/
        ├── main.go                 kingpin entry, subcommands
        └── main_test.go            CLI parse smoke test
```

Mirrors `tools/coverage-check` / `tools/components-gen` (small binary,
`cmd/<name>/main.go` entrypoint, package code in the tool root). The
component-layout rule (`config.go` / `factory.go` / `<name>.go` /
`<name>_test.go` / `README.md` / `example_config.yaml`) is applied
because it's the repo's one-mechanism for "thing with a config and a
factory." No `pkg/` surface — defaults bias toward private. Prompts
live in `prompts/*.tmpl` (edited more often than the code; swappable
without rebuild).

## 2. Core types

```go
// SPDX-License-Identifier: Apache-2.0
package fleet

import (
    "context"
    "errors"
    "time"
)

// Sentinel errors. Same naming convention as lifecycle.ErrAlreadyStarted.
var (
    ErrAlreadyStarted   = errors.New("fleet: already started")
    ErrAgentBudgetSpent = errors.New("fleet: agent token budget exhausted")
    ErrGateRejected     = errors.New("fleet: gate rejected PR")
    ErrSpecAmbiguous    = errors.New("fleet: milestone spec ambiguous")
    ErrLaneFull         = errors.New("fleet: lane at concurrency cap")
)

type RubricKind uint8
const (
    RubricFunctional RubricKind = iota
    RubricNonFunctional
)

type RubricStatus uint8
const (
    RubricOpen     RubricStatus = iota // ☐
    RubricProgress                     // ⧗
    RubricDone                         // ☑
)

type Rubric struct {
    ID       string       // M16.R3
    Text     string
    Status   RubricStatus
    Anchor   string       // file:line into MILESTONES.md
    Kind     RubricKind
}

type MilestoneStatus uint8
const (
    MilestoneOpen MilestoneStatus = iota
    MilestoneInProgress
    MilestoneDone
    MilestoneNeedsHuman
)

type Milestone struct {
    ID          string       // "M16"
    Slug        string       // "kueue-scheduler-receiver"
    Lane        int          // 1..6
    Status      MilestoneStatus
    Rubrics     []Rubric
    DependsOn   []string     // ["M9", "M13"]
    RFC         string       // "0011" or ""
    BlockText   string       // raw MILESTONES.md block, fed to agent
}

type AgentStatus uint8
const (
    AgentPending AgentStatus = iota
    AgentSpawning
    AgentRunning
    AgentPROpen
    AgentGatesRunning
    AgentGatesPassed
    AgentGatesFailed
    AgentRevising
    AgentMerged
    AgentClosed
    AgentEscalated // K-rejection ceiling hit -> "needs human"
)

type Agent struct {
    SessionID      string    // claude session
    MilestoneID    string
    Lane           int
    Branch         string    // "m16-kueue-scheduler-receiver"
    WorktreePath   string    // ".claude/worktrees/m16-..."
    PID            int       // claude CLI process; 0 if reaped
    StartedAt      time.Time
    LastEventAt    time.Time
    IterationCount int
    TokenSpend     int64     // running total, cumulative
    Status         AgentStatus
}

type Verdict uint8
const (
    VerdictPending Verdict = iota
    VerdictPass
    VerdictFail
)

type GateID string
const (
    GateL3 GateID = "L3-rubric"
    GateL4 GateID = "L4-adversarial"
    GateL5 GateID = "L5-drift"
)

type GateResult struct {
    Gate      GateID
    Verdict   Verdict
    HeadSHA   string
    RunAt     time.Time
    ModelUsed string    // "claude-opus-4-7"
    Tokens    int64
    Payload   []byte    // raw JSON from the subagent; archived
    Summary   string    // human-readable one-liner for the PR comment
}

type PR struct {
    Number         int
    HeadSHA        string
    Branch         string
    AgentID        string    // sessionID
    GateResults    []GateResult // most-recent per gate
    RejectionCount int       // L3/L4/L5 fails since last agent push
    Merged         bool
    LastSyncedAt   time.Time
}

type WorkItem struct {
    MilestoneID string
    Lane        int
    Priority    int       // higher = sooner; from dep-graph depth
    BlockedBy   []string  // unsatisfied deps
}

// Lifecycle matches internal/runtime/lifecycle + STYLE.md OTel rule.
type Lifecycle interface {
    Start(ctx context.Context) error    // ErrAlreadyStarted on dup
    Shutdown(ctx context.Context) error // idempotent, bounded
}
```

## 3. State machines

### Per-agent

```
        ┌──────────┐    spawn    ┌──────────┐
        │ pending  │────────────▶│ spawning │
        └──────────┘             └─────┬────┘
                                       │ claude CLI up
                                       ▼
                                 ┌──────────┐
                ┌────────────────│ running  │◀─────────────┐
                │ gh pr create   └──────────┘              │
                ▼                                          │ revise
          ┌─────────┐ orchestrator fires gates ┌────────────────┐
          │ pr_open │─────────────────────────▶│ gates_running  │
          └─────────┘                          └────────┬───────┘
                                                       │
                            ┌──────────────┬───────────┴──────────┐
                            ▼              ▼                      ▼
                     ┌─────────────┐ ┌─────────────┐      ┌────────────┐
                     │gates_passed │ │gates_failed │      │ escalated  │
                     └──────┬──────┘ └──────┬──────┘      └────────────┘
                            │ human merge   │ rejectionCount<K? yes──┐
                            ▼               │ no                     │
                       ┌────────┐           ▼                        │
                       │ merged │      ┌────────────┐                │
                       └────────┘      │ escalated  │                │
                                       └────────────┘                ▼
                                                              back to running
```

Terminal: `merged`, `closed`, `escalated`. Reaper deletes worktree +
session-state file on all three.

### Per-lane (scheduler view)

```
┌────────┐  pickEligible() ┌────────────┐  spawn ┌──────┐
│  idle  │────────────────▶│ launching  │───────▶│ busy │
└────────┘                  └────────────┘        └──┬───┘
    ▲                                                │
    │              agent terminal (merged/closed/escalated)
    └────────────────────────────────────────────────┘
```

Lane `busy` blocks further `pickEligible` until the in-flight agent
hits a terminal state. Default cap: 1; per-lane override in config.

## 4. Durable state — pick sqlite, not state.json

The design doc suggests `tools/fleet/state.json`. **Recommend sqlite
instead** (`modernc.org/sqlite` — pure Go, no cgo, satisfies STYLE.md
"no cgo in core").

**Why not flat JSON.** A `state.json` rewritten per event fails
PRINCIPLES §1. Crash mid-spawn during `os.WriteFile` truncates the
file → restart either re-spawns a duplicate (two claude processes,
same worktree) or loses state. Write-rename closes the corruption
window but not the "process forked, PID not yet recorded" window.
Concurrent writers serialize on a whole-file mutex. No transactional
"record gate result + mark PR ready" primitive.

**Why sqlite.** Pure Go (no cgo, satisfies STYLE.md). Single file on
disk, same operational footprint as `state.json`. Spawn-record is one
transaction: "mark agent spawning + write PID + reserve lane" rolls
back atomically on crash. WAL + `synchronous=NORMAL` survives kill.
Concurrent readers (status subcommand) work while daemon writes.
Schema migrations live in numbered `.sql` files tracked in a
`schema_version` table, verified at startup.

### Schema sketch

```sql
CREATE TABLE schema_version (version INTEGER NOT NULL);
CREATE TABLE agents (
    session_id      TEXT PRIMARY KEY,
    milestone_id    TEXT NOT NULL,
    lane            INTEGER NOT NULL,
    branch          TEXT NOT NULL,
    worktree_path   TEXT NOT NULL,
    pid             INTEGER,
    status          TEXT NOT NULL,
    started_at      INTEGER NOT NULL,
    last_event_at   INTEGER NOT NULL,
    iteration_count INTEGER NOT NULL DEFAULT 0,
    token_spend     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX agents_by_lane ON agents(lane, status);
CREATE TABLE prs (
    number          INTEGER PRIMARY KEY,
    head_sha        TEXT NOT NULL,
    branch          TEXT NOT NULL,
    session_id      TEXT,
    rejection_count INTEGER NOT NULL DEFAULT 0,
    merged          INTEGER NOT NULL DEFAULT 0,
    last_synced_at  INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES agents(session_id)
);
CREATE TABLE gate_results (
    id          INTEGER PRIMARY KEY,
    pr_number   INTEGER NOT NULL,
    gate        TEXT NOT NULL,
    head_sha    TEXT NOT NULL,
    verdict     TEXT NOT NULL,
    model_used  TEXT NOT NULL,
    tokens      INTEGER NOT NULL,
    summary     TEXT,
    payload     BLOB,
    run_at      INTEGER NOT NULL
);
CREATE INDEX gate_results_lookup ON gate_results(pr_number, gate, head_sha);
CREATE TABLE lessons (
    id          INTEGER PRIMARY KEY,
    captured_at INTEGER NOT NULL,
    milestone   TEXT,
    signal      TEXT NOT NULL,
    draft       TEXT NOT NULL,
    pr_number   INTEGER
);
```

Crash semantics: on `Start`, the orchestrator runs a single recovery
query — for every agent in `spawning`, check whether the worktree
exists and the PID is alive; if not, mark `closed` and free the lane.
Three lines of SQL; no ambiguity.

`state.json` lives only as a read-only **export** that the `status`
subcommand and weekly digest derive from the DB. Operators get a
flat-file view without that file being authoritative.

## 5. Concurrency model

- Top-level: `Orchestrator.Start` builds an `errgroup.Group` with a
  context derived from the caller's. Each subsystem (`specWatcher`,
  `prWatcher`, `scheduler`, `gateRunner`, `reaper`) is one `g.Go`.
- Per-lane: scheduler holds a `map[int]*laneState`. Each lane has its
  own buffered channel (size 1) of `WorkItem`. The scheduler pushes;
  a per-lane goroutine pops and runs spawn-then-await.
- PR events flow over one channel: `prEvents chan github.PREvent`,
  produced by `prwatcher`, consumed by `gateRunner`. Gate runner
  fans-out L3/L4/L5 via a sub-errgroup, posts results when all return.
- All goroutines accept `ctx context.Context` and exit on cancel.
  Shutdown deadline 1s per STYLE.md (relaxed at the daemon level to
  10s only to drain GitHub posts; documented).
- No `sync.Once` for shutdown — use the `lifecycle` helper from
  `internal/runtime/lifecycle/` and surface `ErrAlreadyStarted`.
- Banned patterns avoided: no `init()` side effects (factories are
  explicit and constructed in `cmd/fleet/main.go`), no package-level
  globals for the logger (`*slog.Logger` passed via constructor), no
  `pkg/errors`, no `hashicorp/multierror` (use `go.uber.org/multierr`
  on shutdown fan-in only).

## 6. GitHub API surface

Shell out to `gh` (already required for human PR workflows) rather
than embed `go-github`. One-mechanism-over-many: the maintainer's CLI
is the same surface the daemon uses; auth lives in `~/.config/gh/`.
Wrapper in `tools/fleet/github/client.go` so the dependency is
isolated and mockable.

Calls and rates:

| Purpose | Command | Rate |
|---|---|---|
| Discover open PRs | `gh pr list --search "head:m" --json …` | poll every 30s (configurable) |
| PR detail / checks | `gh pr view <n> --json …` | on every state change for that PR |
| Post gate comment | `gh pr comment <n> --body-file …` | once per gate per HeadSHA |
| Read existing comments (idempotency) | `gh api repos/:o/:r/issues/<n>/comments` | once per PR scan |
| Merge state / required checks | `gh pr checks <n>` | piggy-backed on PR view |
| Webhook receive (later) | n/a | optional Phase 4 |

Rate limits: 5000/hr authenticated. Worst-case ~800/hr (6 lanes ×
30s polling + view/comment overhead). On 429-class failures sleep
60s + retry once, then surface `ErrGitHubThrottled` and pause the
watcher one cycle. Skip unchanged PRs via `updated:>…` filter on
`gh pr list` (gh doesn't expose etags).

## 7. Claude CLI invocation

```
claude --print --output-format stream-json \
       --session-id <uuid> \
       --resume <session-id-if-revising> \
       --model claude-opus-4-7 \
       < prompts/milestone.rendered.md \
       > .claude/worktrees/m16-kueue-scheduler-receiver/session.jsonl
```

- One `*exec.Cmd` per agent; `Stdout`/`Stderr` redirected to a per-
  agent log. `Cmd.Wait()` runs in its own goroutine; exit recorded.
- **Per-agent PID file**: `.claude/worktrees/<slug>/agent.pid`,
  written immediately after fork, before the spawn transaction
  commits. Restart compares DB rows to live PIDs.
- Per-agent session-state matches the existing
  `.claude/ralph-loop.local.md` convention — lives in the worktree,
  not in the orchestrator. Orchestrator stores only session ID + PID.
- No systemd; no orchestrator-level PID file. Operator runs
  `bin/fleet run` under whatever supervisor they prefer.
  PRINCIPLES §2: don't ship reversibility-expensive features without
  a user.
- Token accounting: parse `usage` events from the stream-json log;
  hard halt at `cfg.MaxTokensPerAgent` with `ErrAgentBudgetSpent`.
- Gate subagent invocation reuses the same `claude --print` shape
  (fresh session per run) via `gates/claude/invoker.go`.

## 8. Example config

```yaml
# tools/fleet/example_config.yaml
github:
  repo: tracecoreai/tracecore
  default_branch: main
  poll_interval: 30s

worktrees:
  root: .claude/worktrees
  branch_prefix: m

lanes:
  - id: 1
    name: receivers
    concurrency: 1
  - id: 4
    name: scheduler-and-runtime
    concurrency: 1
  - id: 5
    name: benches
    concurrency: 2     # raise only after zero conflicts across ≥20 PRs

agent:
  claude_binary: claude
  model: claude-opus-4-7
  max_iterations: 50
  max_tokens: 1_000_000
  prompt_template: prompts/milestone.tmpl

gates:
  l3:
    model: claude-opus-4-7
    prompt_template: prompts/l3_rubric.tmpl
    timeout: 5m
  l4:
    model: claude-opus-4-7
    prompt_template: prompts/l4_adversarial.tmpl
    timeout: 5m
  l5:
    model: claude-sonnet-4-6
    prompt_template: prompts/l5_drift.tmpl
    timeout: 3m
  max_rejections_per_pr: 3

state:
  driver: sqlite
  path: .fleet/state.db

logging:
  level: info
  format: json
```

`Validate()` rejects: unknown lane IDs in milestones; non-positive
concurrency; missing prompt templates; sqlite path under a non-
writable parent; model strings not on the allowlist.

## 9. Testing strategy

Every file has a `_test.go` per STYLE.md. `tools/` is informational
in `coverage-check`, but target 70% so the harness is as trustworthy
as `internal/`.

Testable without spawning real agents or hitting GitHub:

- **specwatcher**: golden-file MILESTONES.md fixtures → `[]Milestone`.
- **scheduler**: in-memory; eligibility, lane caps, priority order.
- **state**: temp-dir sqlite; transactional spawn path, kill mid-tx
  (simulated), reopen, assert recovery.
- **agent**: a `fake_claude.sh` in `testdata/` prints a deterministic
  stream-json transcript. PID-file, reap, token accounting all
  observable without Anthropic.
- **github/client**: `FakeGH` records argv and replays canned JSON.
  Real `gh` only under `//go:build integration`.
- **gates**: each gate's `Run` is pure (PR diff + rubric → verdict).
  Snapshot tests on `pr_clean` and `pr_broken_missing_citation`
  fixtures. Claude invoker faked.
- **orchestrator**: lifecycle contract — duplicate Start returns
  `ErrAlreadyStarted`; Shutdown idempotent, bounded by 10s.

Race detector on by default. Integration tests under
`//go:build integration` for real `gh` + real `claude` paths.

## 10. Bootstrap path

```bash
# Build & smoke
make fleet-build            # new Makefile target -> go build -o bin/fleet ./tools/fleet/cmd/fleet
make fleet-test             # go test -race ./tools/fleet/...

# Dry run: parses MILESTONES.md, prints what it WOULD spawn, exits.
bin/fleet dryrun --config configs/fleet.yaml

# Single-shot: one lane, one milestone, full loop including gates.
# Spawns one agent against M16, runs gates on the resulting PR, then exits.
bin/fleet run --config configs/fleet.yaml --lane 4 --single-shot --milestone M16

# Status: read-only view from the sqlite DB.
bin/fleet status --config configs/fleet.yaml

# Stop: signal the running daemon (SIGTERM via pidfile or via a small
# control socket — TBD; see open questions).
bin/fleet stop --config configs/fleet.yaml

# Full multi-lane.
bin/fleet run --config configs/fleet.yaml
```

`Makefile` additions:

```make
fleet-build:  ## Build the fleet orchestrator.
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/fleet ./tools/fleet/cmd/fleet

fleet-test:  ## Run fleet unit tests.
	go test -race ./tools/fleet/...
```

No `ci:` change — `make ci` already runs `./...` for vet, lint, and
coverage. The fleet code is in `tools/` and is informational for
coverage, but `make fleet-test` is the explicit gate during Phase 2/3.

## 11. Open implementation questions

1. **Control plane for `fleet stop`.** Pidfile signal is simple but
   loses ack. Unix socket at `.fleet/control.sock` adds ~30 lines and
   gives structured ack. Decide after Phase 2.
2. **Webhooks vs polling.** Phase 2 = polling. Add a
   `fleet webhook-receiver` subcommand later if review burden grows.
3. **Per-lane goroutine vs shared scheduler tick.** Per-lane is
   cleaner but burns 6 idle goroutines. Default to per-lane; revisit
   if pprof shows churn.
4. **Lessons capture: auto-PR vs queue-only.** Initial: capture to
   `lessons` table, never auto-open. Maintainer pulls via
   `bin/fleet status --lessons`. Auto-PR is Phase 4.
5. **`⧗`-in-progress claim convention.** Code treats `⧗` as "skip."
   A `Claimed-by:` line in the milestone block resolves this but
   needs a MILESTONES.md schema bump. Defer to RFC.
6. **Gate prompt versioning.** Record `prompt_sha` per gate result so
   re-runs against a changed prompt are visible.
7. **Worktree GC for orphaned dirs.** Warning, not error: log and
   continue on startup.

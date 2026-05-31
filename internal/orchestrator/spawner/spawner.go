// Package spawner defines the contract between the orchestrator and
// the agent-launch mechanism.
//
// The skeleton ships a Stub implementation that records every Spawn
// call without touching the filesystem or starting a process. The
// production Spawner (`claude --resume` inside an isolated worktree)
// lands in a follow-up commit.
package spawner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Request carries the inputs a Spawner needs to launch an agent.
// Additional fields (prompt template, worktree path, credentials)
// land as the real Spawner takes shape.
type Request struct {
	AgentID    int64
	WorkItemID string
	Lane       string
}

// Result reports the spawned process identifiers the orchestrator
// records in state.agents so a restart can adopt or reap the child.
type Result struct {
	PID       int
	SessionID string
}

// Spawner is the abstraction over agent launch. Implementations MUST
// be safe for concurrent calls; the orchestrator may spawn multiple
// agents in the same tick (one per lane).
type Spawner interface {
	Spawn(ctx context.Context, req Request) (Result, error)
}

// Stub records every Spawn call and returns a synthetic
// (pid, session-id) pair. Used by tests and by `regatta serve` until
// the real spawner ships.
//
// An optional *state.DB enables Complete: when non-nil, terminal
// callback writes the outputs journal entry and flips the work_item
// to merged. The order is load-bearing — scheduler tick step-0
// reads ListPendingEdgesFromMerged then GetLatestOutput, so the
// journal row must exist before the work_item is observed at
// status=merged (spec §3.9).
type Stub struct {
	mu    sync.Mutex
	seq   atomic.Int64
	calls []Request
	db    *state.DB
	log   *slog.Logger
}

// NewStub returns a ready-to-use stub spawner without DB wiring.
// Complete is a no-op on a stub built this way.
func NewStub() *Stub { return &Stub{} }

// NewStubWithDB wires the stub to a *state.DB so Complete can journal
// outputs + mark the work_item merged. The production ClaudeSpawner
// will adopt this same Complete contract once real output capture
// lands.
func NewStubWithDB(db *state.DB) *Stub { return &Stub{db: db} }

// WithLogger installs the structured-event sink for spawn.started /
// spawn.completed / spawn.failed emissions (spec §5.3). Returns the
// receiver for chained construction. A nil logger restores the
// slog.Default() fallback applied by logger().
func (s *Stub) WithLogger(l *slog.Logger) *Stub {
	s.log = l
	return s
}

// logger returns the injected logger or slog.Default() so nil-Config
// consumers still get output without panicking (spec §4.1).
func (s *Stub) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return slog.Default()
}

// Spawn returns a deterministic synthetic Result. PID is a negative
// counter (so it cannot collide with any real OS pid) and SessionID
// embeds the work-item ID for easier debugging.
func (s *Stub) Spawn(_ context.Context, req Request) (Result, error) {
	s.logger().Info(string(obs.EventSpawnStarted),
		string(obs.KeyWorkItemID), req.WorkItemID,
		string(obs.KeyAgentID), req.AgentID,
		string(obs.KeyLane), req.Lane,
	)
	n := s.seq.Add(1)
	s.mu.Lock()
	s.calls = append(s.calls, req)
	s.mu.Unlock()
	return Result{
		PID:       int(-n),
		SessionID: fmt.Sprintf("stub-%d-%s", n, req.WorkItemID),
	}, nil
}

// Calls returns a snapshot of every Spawn request observed.
func (s *Stub) Calls() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.calls))
	copy(out, s.calls)
	return out
}

// Complete is the spawner's terminal callback for one attempt: write
// the outputs journal entry, then mark the work_item merged. Per
// spec §3.9 the journal write must precede the merge transition so
// scheduler tick step-0 (ListPendingEdgesFromMerged → GetLatestOutput)
// never sees a merged work_item whose journal is empty.
//
// AppendOutput canonicalises payload + computes sha256; an invalid
// JSON payload returns an error from canonicalisation and the merge
// transition does NOT run. The two writes share the single sqlite
// connection (state.Open caps the pool at 1) so a concurrent reader
// cannot observe the intermediate (journal-written, status-still-
// running) state. Crash-safety: if the process dies between the
// AppendOutput commit and the UpsertWorkItem commit, the next tick
// observes status!=merged and the orchestrator's reconciliation
// path retries.
func (s *Stub) Complete(ctx context.Context, workItemID string, payload json.RawMessage) error {
	start := time.Now()
	emitFailed := func(err error) error {
		s.logger().Warn(string(obs.EventSpawnFailed),
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyDurationMs), time.Since(start).Milliseconds(),
			string(obs.KeyErr), err.Error(),
		)
		return err
	}
	if s.db == nil {
		return emitFailed(fmt.Errorf("spawner: stub built without DB cannot Complete"))
	}
	if _, err := s.db.AppendOutputAt(ctx, workItemID, payload, time.Now()); err != nil {
		return emitFailed(fmt.Errorf("spawner: append output: %w", err))
	}
	wi, err := s.db.GetWorkItem(ctx, workItemID)
	if err != nil {
		return emitFailed(fmt.Errorf("spawner: load work_item: %w", err))
	}
	wi.Status = state.WorkStatusMerged
	if err := s.db.UpsertWorkItem(ctx, wi, wi.Source, time.Now()); err != nil {
		return emitFailed(fmt.Errorf("spawner: mark merged: %w", err))
	}
	s.logger().Info(string(obs.EventSpawnCompleted),
		string(obs.KeyWorkItemID), workItemID,
		string(obs.KeyDurationMs), time.Since(start).Milliseconds(),
	)
	return nil
}

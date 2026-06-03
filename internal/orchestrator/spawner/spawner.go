// Package spawner defines the orchestrator↔agent-launch contract.
// Ships a Stub implementation that records every Spawn call without
// touching FS or starting a process; the production Spawner
// (`claude --resume` inside an isolated worktree) lands in a follow-up.
package spawner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Request carries the inputs a Spawner needs to launch an agent.
// OperatorID/DAGID/RunID is the cost-governor identifier triple stamped
// onto every token_spend row by SpawnerCallback (#295) so the T4
// reconciler can group per-DAG/per-operator USD. Empty values fall
// back to the WorkItemID-derived shortcut at the writer seam,
// preserving the wave-2 byte-equal contract.
type Request struct {
	AgentID    int64
	WorkItemID string
	Lane       string
	OperatorID string
	DAGID      string
	RunID      string
}

// Result reports spawned process identifiers the orchestrator records
// in state.agents so a restart can adopt or reap the child.
type Result struct {
	PID       int
	SessionID string
}

// Spawner is the agent-launch abstraction. Implementations MUST be
// safe for concurrent calls (orchestrator may spawn multiple agents
// in the same tick).
type Spawner interface {
	Spawn(ctx context.Context, req Request) (Result, error)
}

// Config holds Stub spawner tunables + deps; mirrors the Config.Logger
// DI pattern shared with orchestrator/scheduler/reaper.
type Config struct {
	// DB enables Complete; when non-nil the terminal callback writes
	// the outputs journal then flips work_item to merged. Order is
	// load-bearing — scheduler tick step-0 reads ListPendingEdges
	// FromMerged → GetLatestOutput, so the journal row must exist
	// before status=merged is observed (spec §3.9). Nil makes
	// Complete a no-op error.
	DB *state.DB

	// Logger sinks spawn.started/completed/failed emissions
	// (spec §5.3). Nil falls back to slog.Default().
	Logger *slog.Logger

	// Clock is the time source Complete uses for journal stamps and
	// duration accounting. Nil falls back to time.Now; tests pin
	// deterministic timestamps via this seam (#100).
	Clock func() time.Time

	// Tracer falls back to otel.Tracer("spawner") — noop until
	// obs/otel.Setup runs (W6 spec §3.3).
	Tracer trace.Tracer

	// Meter resolves lazily at ResolveMeter() so a global
	// MeterProvider swap takes effect on the next call.
	// C-T1 + C-T4 wire spawn / classification instruments here.
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or falls back lazily —
// a global provider swap (e.g. test noop injection) takes effect on
// the next call.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeSpawner)
}

// Stub records every Spawn call and returns a synthetic (pid,
// session-id) pair — used by tests and by `regatta serve` until the
// real spawner ships.
type Stub struct {
	mu     sync.Mutex
	seq    atomic.Int64
	calls  []Request
	db     *state.DB
	log    *slog.Logger
	clock  func() time.Time
	tracer trace.Tracer
}

// New constructs a Stub. Mirrors orchestrator/scheduler/reaper
// construction so callers see one injection shape.
func New(cfg Config) *Stub {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("spawner")
	}
	return &Stub{db: cfg.DB, log: log, clock: clock, tracer: tracer}
}

// Spawn returns a deterministic synthetic Result. PID is a negative
// counter so it cannot collide with any real OS pid; SessionID embeds
// the work-item ID for debugging.
func (s *Stub) Spawn(ctx context.Context, req Request) (Result, error) {
	// W6 spec §3.5: one `operator_invocation` span per spawn, parent
	// of the T4 `llm_call` child opened by the stream-json parser.
	_, span := s.tracer.Start(ctx, "operator_invocation",
		trace.WithAttributes(
			attribute.Int64(string(obs.KeyAgentID), req.AgentID),
			attribute.String(string(obs.KeyWorkItemID), req.WorkItemID),
			attribute.String(string(obs.KeyLane), req.Lane),
		))
	defer span.End()
	s.log.Info(string(obs.EventSpawnStarted),
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

// Complete is the spawner's terminal callback: write outputs journal,
// then mark the work_item merged. Order is load-bearing per spec
// §3.9 — scheduler tick step-0 (ListPendingEdgesFromMerged →
// GetLatestOutput) never sees a merged work_item whose journal is
// empty. AppendOutput canonicalises + sha256s the payload; invalid
// JSON aborts before the merge transition. The two writes share the
// single sqlite connection (pool cap=1) so a concurrent reader cannot
// observe the intermediate (journal-written, status-still-running)
// state. Crash between commits → next tick observes status!=merged
// and reconciliation retries.
func (s *Stub) Complete(ctx context.Context, workItemID string, payload json.RawMessage) error {
	start := s.clock()
	emitFailed := func(err error) error {
		s.log.Warn(string(obs.EventSpawnFailed),
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyDurationMs), s.clock().Sub(start).Milliseconds(),
			string(obs.KeyErr), err.Error(),
		)
		return err
	}
	if s.db == nil {
		return emitFailed(fmt.Errorf("spawner: stub built without DB cannot Complete"))
	}
	if _, err := s.db.AppendOutputAt(ctx, workItemID, payload, s.clock()); err != nil {
		return emitFailed(fmt.Errorf("spawner: append output: %w", err))
	}
	wi, err := s.db.GetWorkItem(ctx, workItemID)
	if err != nil {
		return emitFailed(fmt.Errorf("spawner: load work_item: %w", err))
	}
	wi.Status = state.WorkStatusMerged
	if err := s.db.UpsertWorkItem(ctx, wi, wi.Source, s.clock()); err != nil {
		return emitFailed(fmt.Errorf("spawner: mark merged: %w", err))
	}
	s.log.Info(string(obs.EventSpawnCompleted),
		string(obs.KeyWorkItemID), workItemID,
		string(obs.KeyDurationMs), s.clock().Sub(start).Milliseconds(),
	)
	return nil
}

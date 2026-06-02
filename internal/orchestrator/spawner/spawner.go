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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Request carries the inputs a Spawner needs to launch an agent.
// Additional fields (prompt template, worktree path, credentials)
// land as the real Spawner takes shape.
//
// OperatorID/DAGID/RunID are the cost-governor identifier triple stamped
// onto every token_spend row by SpawnerCallback (#295). The orchestrator
// populates them from agent + work_item context; the substrate row carries
// three distinct columns so the T4 reconciler and per-DAG/per-operator
// USD rollups can group correctly. Empty values are accepted — callers
// that have not yet been threaded fall back to the WorkItemID-derived
// shortcut at the writer seam, preserving the wave-2 byte-equal contract.
type Request struct {
	AgentID    int64
	WorkItemID string
	Lane       string
	OperatorID string
	DAGID      string
	RunID      string
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

// Config holds tunables and dependencies for a Stub spawner.
// Mirrors the Config.Logger DI pattern used by orchestrator, scheduler,
// and reaper so all components share one injection shape.
type Config struct {
	// DB enables Complete: when non-nil, terminal callback writes the
	// outputs journal entry and flips the work_item to merged. The
	// order is load-bearing — scheduler tick step-0 reads
	// ListPendingEdgesFromMerged then GetLatestOutput, so the journal
	// row must exist before the work_item is observed at status=merged
	// (spec §3.9). Optional; nil makes Complete a no-op error.
	DB *state.DB

	// Logger is the structured-event sink for spawn.started /
	// spawn.completed / spawn.failed emissions (spec §5.3). Nil falls
	// back to slog.Default() so embedded callers still get output
	// without panicking (spec §4.1).
	Logger *slog.Logger

	// Clock is the time source Complete consumes for journal stamps and
	// duration accounting. Mirrors the func()-clock injection used by
	// state.OpenWithClock and gates/approval — the seam tests need to
	// pin deterministic timestamps (#100). Nil falls back to time.Now so
	// production callers stay zero-config.
	Clock func() time.Time

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("spawner") which resolves to the
	// global provider — noop until obs/otel.Setup runs. Per W6 spec
	// §3.3 + feedback_spec_pattern_authority.
	Tracer trace.Tracer

	// Meter is the OTel instrument factory for spawner telemetry.
	// Nil resolves to otel.Meter("spawner") at the first
	// ResolveMeter() call so the global MeterProvider Setup wires (or
	// a noop when Setup was skipped) wins by default. Mirrors the W6
	// Config.Tracer pattern so callers stay on one DI seam across
	// trace + metric. C-T1 + C-T4 wire spawn / classification
	// instruments here.
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. The fallback is lazy so a global
// provider swap (e.g. test injection of a noop provider) takes effect
// on the next call. Matches the W6 Config.Tracer nil-fallback shape.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter("spawner")
}

// Stub records every Spawn call and returns a synthetic
// (pid, session-id) pair. Used by tests and by `regatta serve` until
// the real spawner ships.
type Stub struct {
	mu     sync.Mutex
	seq    atomic.Int64
	calls  []Request
	db     *state.DB
	log    *slog.Logger
	clock  func() time.Time
	tracer trace.Tracer
}

// New constructs a Stub from a Config. Mirrors orchestrator/scheduler/
// reaper construction so callers see one injection shape.
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
// counter (so it cannot collide with any real OS pid) and SessionID
// embeds the work-item ID for easier debugging.
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

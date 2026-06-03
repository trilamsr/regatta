// Package scheduler picks pending agents that are eligible to spawn
// and reserves their lanes + hotspot locks. One Tick is non-blocking;
// the orchestrator's Run loop calls Tick on a timer and feeds returned
// agent IDs into the spawner. Hotspot locks acquire in lexicographic
// order (docs/design.md §Concurrency); lane concurrency gates against
// the configured per-lane cap.
package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/gates/l4"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// CostGate is the scheduler-side seam to the cost-governor pre-call
// deny primitive — exists so tests can inject fakes without pulling
// in spend.Reader + substrate fixtures.
type CostGate interface {
	Evaluate(ctx context.Context, scope gate.WorkItemScope) (gate.Verdict, error)
}

// CostGateResolver maps a planned work_item to its cost scope; ok=false
// means out-of-scope (lane outside the cost regime).
type CostGateResolver func(wi state.WorkItem) (gate.WorkItemScope, bool)

// L4Gate is the scheduler-side seam to the adversarial-reviewer gate —
// keeps the model adapter + prompt loader + OTel emitter transitive
// deps invisible to scheduler tests. Spec §3.2 step 0.7.
type L4Gate interface {
	Evaluate(ctx context.Context, cfg l4.Config, in l4.Input) (schemas.GateResult, error)
}

// L4GateResolver maps a planned work_item to its L4 config + Input;
// ok=false means out-of-scope (e.g. doc-only PRs). Input construction
// (diff vs base, scorecard extraction) lives in production wiring so
// the scheduler stays policy-agnostic.
type L4GateResolver func(wi state.WorkItem) (l4.Config, l4.Input, bool)

// DowngradeHook surfaces a soft-cap-induced model swap to the spawner.
// Nil is legal — soft-cap downgrades degrade to WARN-only.
type DowngradeHook func(workItemID, model string)

// EdgeEvaluator is defined here rather than imported from program to
// avoid an import cycle: program imports orchestrator for predicate
// sentinels and orchestrator imports scheduler. Schema is opaque (any)
// because type-checking against OutputsSchema happens at brief-load
// time (ValidateV2 in program), not at the runtime seam.
type EdgeEvaluator interface {
	Eval(ctx context.Context, edge state.EdgeRow, schema any, journal state.OutputJournalEntry) (bool, string, error)
}

// OutputsSchemaResolver maps an upstream feature ID to the live
// OutputsSchema; (nil, false) is treated as "no schema", matching the
// runtime evaluator's contract.
type OutputsSchemaResolver func(featureID string) (any, bool)

// HotspotResolver maps a work-item ID to the hotspot lock names it
// touches. Empty slice is valid for items that touch no hotspots.
type HotspotResolver func(workItemID string) []string

// ApprovalGate is the scheduler-side seam to the HITL gate — keeps the
// gate's transitive deps (approvaltoken.Keyring, notifier, slog) invisible to
// scheduler tests.
type ApprovalGate interface {
	Evaluate(ctx context.Context, wi state.WorkItem, cfg approval.Config) (approval.Result, error)
}

// GateResolver maps a planned work_item to its approval gate config;
// (_, false) means non-gated. Resolution policy (by-lane, by-tag,
// predicate-CEL) lives in serve.go so the scheduler stays policy-agnostic.
type GateResolver func(wi state.WorkItem) (approval.Config, bool)

// Config holds tunables derived from regatta.yaml at orchestrator
// construction time.
type Config struct {
	// LaneCaps gives the max active agents per lane; missing lane means
	// unlimited. Default lane uses the empty-string key.
	LaneCaps map[string]int

	// LockTTL is the heartbeat lease passed to TryAcquireLocks; a lock
	// whose heartbeat is older than LockTTL may be stolen.
	LockTTL time.Duration

	// Hotspots resolves an item to its hotspot lock names; nil disables
	// lock acquisition entirely.
	Hotspots HotspotResolver

	// Evaluator runs CEL predicates; nil makes scheduler behave like its
	// MVP-1 self (depends_on_features only). Shared across ticks so its
	// compile cache survives.
	Evaluator EdgeEvaluator

	// OutputsSchemas resolves an upstream feature's OutputsSchema for
	// runtime predicate eval. nil is legal; runtime evaluator does not
	// consult schema (reserved for forward-compat).
	OutputsSchemas OutputsSchemaResolver

	// Logger is the structured-event sink; nil falls back to
	// slog.Default() (spec §4.1).
	Logger *slog.Logger

	// Gate evaluates the HITL approval gate-pass (spec §3.1 step 0.5);
	// nil disables the pass.
	Gate ApprovalGate

	// GateResolver maps wi to gate config; nil disables the pass. Same
	// semantics as Gate=nil so a half-wired runtime fails cleanly.
	GateResolver GateResolver

	// Tracer is the OTel tracer; nil falls back to otel.Tracer scoped
	// to the global provider — noop until obs/otel.Setup runs.
	Tracer trace.Tracer

	// Meter is the OTel instrument factory; nil resolves lazily at
	// ResolveMeter() so a global MeterProvider swap takes effect.
	Meter metric.Meter

	// CostGate evaluates the cost-governor pre-call deny gate (spec
	// §3.2 step 0.6); nil short-circuits applyCostGovernor to identity
	// for zero overhead.
	CostGate CostGate

	// CostGateResolver maps wi to cost scope; nil disables the pass.
	CostGateResolver CostGateResolver

	// OnDowngrade fires once per Evaluate that returns
	// SoftCapBreached=true with a DowngradeTo target AND a scope that
	// opted in via AllowDowngrade. Spawner-side ModelOverride consumer
	// lands in Wave 2.
	OnDowngrade DowngradeHook

	// L4Gate evaluates the adversarial-reviewer gate-pass (spec §3.2
	// step 0.7); nil short-circuits applyL4Gate to identity.
	L4Gate L4Gate

	// L4GateResolver maps wi to L4 config + Input; nil disables the pass.
	L4GateResolver L4GateResolver

	// CostCap is the global daily-spend ceiling (spec PHASE-AUTONOMY W5).
	// Allow=false halts the entire tick BEFORE any per-scope gate runs.
	// nil short-circuits applyCostCap to identity (zero overhead).
	CostCap CostCapGate

	// Clock is the time source for tick + step latency measurement;
	// nil falls back to time.Now (production wiring). Injection seam
	// is the same shape as the rest of regatta (state.OpenWithClock,
	// spawner.Config.Clock, web.Dependencies.Clock) so tests that
	// already fix the state-layer clock can pin the scheduler's loop
	// clock to the same instant for fully deterministic latency
	// histograms.
	Clock func() time.Time

	// MergeCoordinator + MergeWorker wire the gates_pass → auto-merge
	// path (#612). Both nil = auto-merge disabled (--auto-merge=false
	// default) so OnGatesPass becomes a no-op and the daemon stays
	// operator-observable-equivalent to pre-c2.
	MergeCoordinator *merge.Coordinator
	MergeWorker      *merge.Worker
}

// ResolveMeter returns the configured meter or falls back lazily so a
// global provider swap (e.g. test noop injection) takes effect on the
// next call.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeScheduler)
}

// schedulerDB is the seam between Scheduler and state.DB so crash-
// injection tests can wrap the real DB to drop specific writes mid-tick
// (Seam-2 in docs/superpowers/specs/2026-05-31-fix-98-scheduler-default-fallback.md §5.1).
type schedulerDB interface {
	ListSpawnable(ctx context.Context) ([]state.WorkItem, error)
	UpsertPending(ctx context.Context, workItemID, lane string) (*state.Agent, error)
	UpsertPendingTx(ctx context.Context, tx *sql.Tx, workItemID, lane string) (*state.Agent, error)
	ListAgentsByState(ctx context.Context, states ...state.AgentState) ([]state.Agent, error)
	CountAgentsByLane(ctx context.Context, states ...state.AgentState) (map[string]int, error)
	TransitionAgent(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error)
	TransitionAgentTx(ctx context.Context, tx *sql.Tx, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error)
	TryAcquireLocks(ctx context.Context, names []string, agentID int64, ttl time.Duration) error
	TryAcquireLocksTx(ctx context.Context, tx *sql.Tx, names []string, agentID int64, ttl time.Duration) error
	ReleaseAgentLocks(ctx context.Context, agentID int64) (int64, error)
	ExpireStaleLocks(ctx context.Context, ttl time.Duration) (int64, error)
	ListPendingEdgesFromMerged(ctx context.Context) ([]state.EdgeRow, error)
	ListEdgesFrom(ctx context.Context, fromID string) ([]state.EdgeRow, error)
	CountNonDefaultEdgeStates(ctx context.Context, fromID string) (state.EdgeFromAggregate, error)
	MarkEdgeFired(ctx context.Context, edgeID int64, fired, contentSHA string) error
	GetLatestOutput(ctx context.Context, workItemID string) (state.OutputJournalEntry, error)
	WithTx(ctx context.Context, fn func(*sql.Tx) error) error
	// TransitionWorkItem CAS-updates work_items.status — consolidates
	// the approval-gate reject path and brief_loader cascade-archive
	// onto one primitive (formerly both issued raw-SQL UPDATEs).
	TransitionWorkItem(ctx context.Context, id string, from, to state.WorkItemStatus) error
	// GetAgentByWorkItemID + RecordEvent are the seam applyL4Gate uses
	// to emit gate_rejected audit rows (issue #479). agent_id=NULL when
	// no row exists yet so the gate can fire pre-spawn.
	GetAgentByWorkItemID(ctx context.Context, workItemID string) (*state.Agent, error)
	RecordEvent(ctx context.Context, agentID int64, kind, payloadJSON string) error
}

// Scheduler is single-caller: Tick must not run concurrently. The
// orchestrator's Run loop owns the only call site and is flock-guarded
// by PollOnce, so this holds without an explicit mutex. Admin commands
// that want to peek at state must use read-only state.DB queries
// rather than calling Tick.
type Scheduler struct {
	db     schedulerDB
	cfg    Config
	log    *slog.Logger
	tracer trace.Tracer

	// tickLatency + stepDuration are the §3 row 4 / A-T3 histograms,
	// pre-created so the hot path is a single Record() with no per-tick
	// allocation.
	tickLatency  metric.Float64Histogram
	stepDuration metric.Float64Histogram

	// multiDefaultLogged dedupes edge.multiple_defaults_per_from so a
	// misconfigured brief logs once per (program_id, from_id) per
	// scheduler-process lifetime rather than every tick.
	multiDefaultLogged sync.Map

	// WriteHook is test-only — see
	// docs/engineer/specs/2026-06-02-s3-t4-crash-recovery-property.md
	// §3.3. Production wires nil.
	WriteHook func(writeIndex int) error
}

// New constructs a Scheduler. Config is copied; later mutations to
// cfg.LaneCaps do not affect the running scheduler.
func New(db *state.DB, cfg Config) *Scheduler {
	return newScheduler(db, cfg)
}

// newWithDB is the test-only constructor accepting the schedulerDB
// interface so crash-injection tests can wrap *state.DB.
func newWithDB(db schedulerDB, cfg Config) *Scheduler {
	return newScheduler(db, cfg)
}

func newScheduler(db schedulerDB, cfg Config) *Scheduler {
	caps := make(map[string]int, len(cfg.LaneCaps))
	maps.Copy(caps, cfg.LaneCaps)
	cfg.LaneCaps = caps
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = 15 * time.Minute
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("scheduler")
	}
	meter := cfg.ResolveMeter()
	tickLatency, err := meter.Float64Histogram("regatta.scheduler.tick.latency_ms")
	if err != nil {
		// Construction failure on a noop or in-process SDK is
		// unrecoverable here; fall back to noop so Tick stays hot.
		tickLatency, _ = obs.Meter(obs.MeterScopeSchedulerFallback).Float64Histogram("regatta.scheduler.tick.latency_ms")
	}
	stepDuration, err := meter.Float64Histogram("regatta.scheduler.tick.step_duration_ms")
	if err != nil {
		stepDuration, _ = obs.Meter(obs.MeterScopeSchedulerFallback).Float64Histogram("regatta.scheduler.tick.step_duration_ms")
	}
	return &Scheduler{
		db: db, cfg: cfg, log: log, tracer: tracer,
		tickLatency: tickLatency, stepDuration: stepDuration,
	}
}

// activeStates lists agent states that count against a lane's cap.
// Pending agents don't count (they're candidates, not active workers);
// terminal states never count.
var activeStates = []state.AgentState{
	state.AgentSpawning,
	state.AgentRunning,
	state.AgentPROpen,
	state.AgentGatesRunning,
	state.AgentAwaitingMerge,
	state.AgentGatesFailed,
}

// Tick performs one scheduling pass. Reads work_items via
// ListSpawnable (universal-queue source of truth per spec §2.8),
// applies the gate-pass chain (approval → cost → l4), then reserves
// lanes + hotspot locks for survivors.
//
// Per-agent reservation runs as a single sqlite tx — UpsertPending +
// TryAcquireLocks + TransitionAgent commit atomically or roll back as
// a unit (issue #88). Orphan-row safety is preserved via two paths:
// ListSpawnable filters a.id IS NULL so a wi with any agent row stops
// re-emitting, and ListAgentsByState(AgentPending) re-discovers any
// agent upserted but not transitioned (lane-capped, hotspot-blocked).
func (s *Scheduler) Tick(ctx context.Context) (reserved []int64, err error) {
	tickStart := s.cfg.Clock()
	defer func() {
		s.tickLatency.Record(ctx, float64(s.cfg.Clock().Sub(tickStart).Microseconds())/1000.0)
	}()

	tc := &tickCtx{}
	var spawnable []state.WorkItem
	var occupancy map[string]int
	var attempted map[int64]struct{}

	// Spec §2.3 + §4 trap #2: ONE span around the step loop with an
	// iteration counter — NOT one span per step.
	steps := []struct {
		name string
		fn   func() error
	}{
		{"fold", func() error {
			if s.cfg.Evaluator == nil {
				return nil
			}
			if e := s.evalPendingEdges(ctx, tc); e != nil {
				return fmt.Errorf("scheduler: eval edges: %w", e)
			}
			return nil
		}},
		{"reaper", func() error {
			if _, e := s.db.ExpireStaleLocks(ctx, s.cfg.LockTTL); e != nil {
				return fmt.Errorf("scheduler: expire stale locks: %w", e)
			}
			return nil
		}},
		{"gate_l0", func() error {
			sp, e := s.db.ListSpawnable(ctx)
			if e != nil {
				return fmt.Errorf("scheduler: list spawnable: %w", e)
			}
			spawnable = sp
			return nil
		}},
		// gate_cost_cap runs BEFORE per-scope evaluation so a saturated
		// 24h budget halts the entire tick — saves the approval/cost/l4
		// passes their per-candidate work when nothing is going to spawn
		// anyway. Spec PHASE-AUTONOMY W5 §5.
		{"gate_cost_cap", func() error {
			spawnable = s.applyCostCap(ctx, spawnable)
			return nil
		}},
		{"gate_approval", func() error {
			sp, e := s.applyApprovalGates(ctx, tc, spawnable)
			if e != nil {
				return fmt.Errorf("scheduler: apply approval gates: %w", e)
			}
			spawnable = sp
			return nil
		}},
		// gate_cost runs BEFORE l4 so we never pay model tokens for
		// cost-denied wi.
		{"gate_cost", func() error {
			sp, e := s.applyCostGovernor(ctx, spawnable)
			if e != nil {
				return fmt.Errorf("scheduler: apply cost governor: %w", e)
			}
			spawnable = sp
			return nil
		}},
		{"gate_l4", func() error {
			sp, e := s.applyL4Gate(ctx, spawnable)
			if e != nil {
				return fmt.Errorf("scheduler: apply l4 gate: %w", e)
			}
			spawnable = sp
			return nil
		}},
		{"dispatch", func() error {
			occ, e := s.db.CountAgentsByLane(ctx, activeStates...)
			if e != nil {
				return e
			}
			occupancy = occ
			r, att, e := s.reserveFromSpawnable(ctx, tc, spawnable, occupancy)
			reserved = r
			attempted = att
			return e
		}},
		// persist: orphan re-reservation — lane-capped and lock-held
		// pending rows from prior ticks (spec §3.2 step 0.9).
		{"persist", func() error {
			rest, e := s.reserveOrphans(ctx, tc, occupancy, attempted)
			if e != nil {
				return e
			}
			reserved = append(reserved, rest...)
			return nil
		}},
	}

	loopCtx, loopSpan := s.tracer.Start(ctx, "tick_steps",
		trace.WithAttributes(attribute.Int("step_count", len(steps))))
	defer loopSpan.End()

	for _, step := range steps {
		stepStart := s.cfg.Clock()
		stepErr := step.fn()
		s.stepDuration.Record(loopCtx,
			float64(s.cfg.Clock().Sub(stepStart).Microseconds())/1000.0,
			metric.WithAttributes(attribute.String("step", step.name)))
		if stepErr != nil {
			return reserved, stepErr
		}
	}
	return reserved, nil
}

// tickCtx threads the per-tick WriteHook counter through Tick's
// substrate-bound writes. Heap-once allocation cost is negligible vs.
// sqlite-tx overhead.
type tickCtx struct{ writeIndex int }

// writeHookErr wraps a non-nil WriteHook return so per-item err
// swallow inside reserveFromSpawnable can errors.Is-detect the
// crash-sim path and propagate it up. Production WriteHook=nil never
// constructs this.
type writeHookErr struct{ inner error }

func (e *writeHookErr) Error() string { return "scheduler: write-hook aborted: " + e.inner.Error() }
func (e *writeHookErr) Unwrap() error { return e.inner }

// fireWriteHook is the test-only crash-injection seam (spec §3.3). Nil
// hook short-circuits with one branch + one pointer-deref, no allocs.
// writeIndex increments even when nil so counter meaning is
// independent of test wiring.
func (s *Scheduler) fireWriteHook(tc *tickCtx) error {
	idx := tc.writeIndex
	tc.writeIndex++
	if s.WriteHook == nil {
		return nil
	}
	if err := s.WriteHook(idx); err != nil {
		return &writeHookErr{inner: err}
	}
	return nil
}

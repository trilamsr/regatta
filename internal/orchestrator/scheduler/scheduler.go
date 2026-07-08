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
// deny primitive — lets tests inject fakes without pulling in
// spend.Reader + substrate fixtures.
type CostGate interface {
	Evaluate(ctx context.Context, scope gate.WorkItemScope) (gate.Verdict, error)
}

// CostGateResolver maps wi to its cost scope; ok=false means out-of-
// scope (lane outside the cost regime).
type CostGateResolver func(wi state.WorkItem) (gate.WorkItemScope, bool)

// L4Gate is the scheduler-side seam to the adversarial-reviewer gate
// (spec §3.2 step 0.7); keeps the model adapter + prompt loader + OTel
// emitter deps invisible to scheduler tests.
type L4Gate interface {
	Evaluate(ctx context.Context, cfg l4.Config, in l4.Input) (schemas.GateResult, error)
}

// L4GateResolver maps wi to its L4 scope; ok=false means out-of-scope.
// Returning L4Scope directly lets filter.Pass[L4Scope] consume the
// resolver without an adapter wrapper (#704 R1).
type L4GateResolver func(wi state.WorkItem) (L4Scope, bool)

// DowngradeHook surfaces a soft-cap-induced model swap to the spawner.
// Nil is legal — soft-cap downgrades degrade to WARN-only.
type DowngradeHook func(workItemID, model string)

// EdgeEvaluator is declared here rather than imported from program to
// avoid an import cycle. Schema is opaque (any) because OutputsSchema
// type-checks happen at brief-load (ValidateV2 in program), not at
// the runtime seam.
type EdgeEvaluator interface {
	Eval(ctx context.Context, edge state.EdgeRow, schema any, journal state.OutputJournalEntry) (bool, string, error)
}

// OutputsSchemaResolver maps an upstream feature ID to the live
// OutputsSchema; (nil, false) means "no schema".
type OutputsSchemaResolver func(featureID string) (any, bool)

// HotspotResolver maps a work-item ID to its hotspot lock names; empty
// slice is valid.
type HotspotResolver func(workItemID string) []string

// ApprovalGate is the scheduler-side seam to the HITL gate; keeps
// approvaltoken.Keyring + notifier + slog invisible to scheduler tests.
type ApprovalGate interface {
	Evaluate(ctx context.Context, wi state.WorkItem, cfg approval.Config) (approval.Result, error)
}

// GateResolver maps wi to its approval gate config; (_, false) means
// non-gated. Resolution policy lives in serve.go so scheduler stays
// policy-agnostic.
type GateResolver func(wi state.WorkItem) (approval.Config, bool)

// Config holds tunables derived from regatta.yaml at orchestrator
// construction time.
type Config struct {
	// LaneCaps: max active agents per lane; missing = unlimited.
	// Default lane uses the empty-string key.
	LaneCaps map[string]int

	// ParallelCap is the aggregate ceiling across ALL lanes for one
	// Tick (spec 2026-06-09 §3.1; closes #1169). Zero = disabled
	// (backward-compat: lane-cap-only semantics). When > 0 and
	// runningAgents + len(spawnable) > ParallelCap, the scheduler
	// truncates the spawnable slice after gate_l0 and BEFORE the
	// cost / approval / l4 gates so per-tick gate-evaluation cost
	// scales with the cap, not queue depth (#1172 tick.slow).
	// Lane caps still bind first; ParallelCap applies on top of the
	// already-lane-filtered set.
	ParallelCap int

	// LockTTL is the heartbeat lease for TryAcquireLocks; older locks
	// may be stolen.
	LockTTL time.Duration

	// Hotspots resolves an item to its hotspot lock names; nil disables
	// lock acquisition.
	Hotspots HotspotResolver

	// Evaluator runs CEL predicates; nil falls back to MVP-1 behaviour
	// (depends_on_features only). Shared across ticks for compile cache.
	Evaluator EdgeEvaluator

	// OutputsSchemas resolves an upstream feature's OutputsSchema for
	// runtime predicate eval; nil is legal (schema is advisory).
	OutputsSchemas OutputsSchemaResolver

	// Logger nil falls back to slog.Default() (spec §4.1).
	Logger *slog.Logger

	// Gate / GateResolver: spec §3.1 step 0.5 HITL approval gate-pass;
	// either nil disables the pass so a half-wired runtime fails cleanly.
	Gate         ApprovalGate
	GateResolver GateResolver

	// Tracer nil falls back to otel.Tracer on the global provider
	// (noop until obs/otel.Setup runs).
	Tracer trace.Tracer

	// Meter nil resolves lazily at ResolveMeter() so a global
	// MeterProvider swap takes effect on the next call.
	Meter metric.Meter

	// CostGate / CostGateResolver: spec §3.2 step 0.6 cost-governor
	// pre-call deny gate; either nil short-circuits applyCostGovernor
	// to identity (zero overhead).
	CostGate         CostGate
	CostGateResolver CostGateResolver

	// OnDowngrade fires once per Evaluate that returns
	// SoftCapBreached=true with a DowngradeTo target AND a scope that
	// opted in via AllowDowngrade.
	OnDowngrade DowngradeHook

	// L4Gate / L4GateResolver: spec §3.2 step 0.7 adversarial-reviewer
	// gate-pass; either nil short-circuits applyL4Gate to identity.
	L4Gate         L4Gate
	L4GateResolver L4GateResolver

	// CostCap is the global daily-spend ceiling (PHASE-AUTONOMY W5).
	// Allow=false halts the whole tick BEFORE per-scope gates; nil
	// short-circuits to identity.
	CostCap CostCapGate

	// Clock nil falls back to time.Now. Injection seam mirrors
	// state.OpenWithClock + spawner.Config.Clock so tests pin both
	// layers to the same instant for deterministic latency histograms.
	Clock func() time.Time

	// MergeCoordinator + MergeWorker wire the gates_pass → auto-merge
	// path (#612). Both nil = auto-merge disabled.
	MergeCoordinator *merge.Coordinator
	MergeWorker      *merge.Worker

	// LowRiskGate filters which gates-passed PRs may auto-merge (MAY-86).
	// nil keeps OnGatesPass byte-equivalent to the pre-MAY-86 path
	// (every PR proceeds); a wired gate holds load-bearing / over-cap /
	// un-soaked PRs for an operator glance. The conservative default
	// (--auto-merge=true, low-risk disabled) wires a hold-all gate so
	// auto-merge never widens past the pre-MAY-86 surface.
	LowRiskGate LowRiskGate

	// RecheckBackoff*: orphan fetch-failure backoff knobs (#794). Zero
	// picks legacy defaults (K=3, N=10, stale=20); sub-1 values clamp.
	RecheckBackoffK             int
	RecheckBackoffSuppressTicks int
	RecheckBackoffStaleTicks    int

	// FileScopeExtractor projects a candidate's predicted file paths so
	// the dispatcher can defer same-lane siblings that touch a shared
	// file (#1065). nil keeps pre-#1065 behavior (lane-cap only).
	FileScopeExtractor FileScopeExtractor
}

// ResolveMeter returns Meter or a lazily-resolved global fallback.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeScheduler)
}

// recheckBackoffConfig projects Config's RecheckBackoff* knobs onto the
// helper-local struct (#794). Clamping lives in resolveRecheckBackoffConfig.
func (c Config) recheckBackoffConfig() recheckBackoffConfig {
	return recheckBackoffConfig{
		K:             c.RecheckBackoffK,
		SuppressTicks: c.RecheckBackoffSuppressTicks,
		StaleTicks:    c.RecheckBackoffStaleTicks,
	}
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
// MUST use read-only state.DB queries rather than calling Tick.
type Scheduler struct {
	db     schedulerDB
	cfg    Config
	log    *slog.Logger
	tracer trace.Tracer

	// Pre-created (§3 row 4 / A-T3) so the hot path is a single
	// Record() with no per-tick allocation.
	tickLatency  metric.Float64Histogram
	stepDuration metric.Float64Histogram

	// multiDefaultLogged dedupes edge.multiple_defaults_per_from so a
	// misconfigured brief logs once per (program_id, from_id) per
	// process — not every tick.
	multiDefaultLogged sync.Map

	// WriteHook is test-only — see
	// docs/engineer/specs/2026-06-02-s3-t4-crash-recovery-property.md
	// §3.3. Production wires nil.
	WriteHook func(writeIndex int) error

	backoff *recheckBackoff
}

// New constructs a Scheduler. Config is copied; later mutations to
// cfg.LaneCaps do not affect the running scheduler.
func New(db *state.DB, cfg Config) *Scheduler {
	return newScheduler(db, cfg)
}

// newWithDB lets crash-injection tests wrap *state.DB.
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
		// Fall back to noop so Tick stays hot when the SDK rejects
		// histogram creation.
		tickLatency, _ = obs.Meter(obs.MeterScopeSchedulerFallback).Float64Histogram("regatta.scheduler.tick.latency_ms")
	}
	stepDuration, err := meter.Float64Histogram("regatta.scheduler.tick.step_duration_ms")
	if err != nil {
		stepDuration, _ = obs.Meter(obs.MeterScopeSchedulerFallback).Float64Histogram("regatta.scheduler.tick.step_duration_ms")
	}
	return &Scheduler{
		db: db, cfg: cfg, log: log, tracer: tracer,
		tickLatency: tickLatency, stepDuration: stepDuration,
		backoff: newRecheckBackoffWithMeter(meter),
	}
}

// activeStates count against a lane's cap. Pending is a candidate,
// not a worker; terminal states never count.
var activeStates = []state.AgentState{
	state.AgentSpawning,
	state.AgentRunning,
	state.AgentPROpen,
	state.AgentGatesRunning,
	state.AgentAwaitingMerge,
	state.AgentGatesFailed,
}

// Tick performs one scheduling pass: ListSpawnable (universal-queue
// source of truth, spec §2.8) → gate chain (approval → cost → l4) →
// reserve lanes + hotspot locks for survivors.
//
// Per-agent reservation is one sqlite tx (UpsertPending +
// TryAcquireLocks + TransitionAgent commit atomically or roll back;
// issue #88). Orphan-row safety: ListSpawnable filters a.id IS NULL
// so wis with any agent row stop re-emitting, while
// ListAgentsByState(AgentPending) re-discovers lane-capped /
// hotspot-blocked upserts.
func (s *Scheduler) Tick(ctx context.Context) (reserved []int64, err error) {
	tickStart := s.cfg.Clock()
	defer func() {
		s.tickLatency.Record(ctx, float64(s.cfg.Clock().Sub(tickStart).Microseconds())/1000.0)
	}()

	tc := &tickCtx{laneCaps: s.snapshotLaneCaps()}
	var spawnable []state.WorkItem
	var occupancy map[string]int
	var attempted map[int64]struct{}
	// l0Count is the spawnable set size right after gate_l0, before any
	// downstream gate prunes it. The dispatch step compares len(reserved)
	// against this to detect the #1218 starved-tick signal (spawnable
	// >0 but reservation produced zero) regardless of WHICH gate
	// blocked dispatch.
	var l0Count int

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
			l0Count = len(sp)
			return nil
		}},
		// snapshot_work_items batches the WorkItem fetches that
		// buildActiveFileScopes (dispatch) and recheckGates (persist)
		// otherwise issue per-agent, collapsing an N+1 GetWorkItem loop
		// into ONE SELECT per tick (#1359, R31-I5).
		{"snapshot_work_items", func() error {
			s.snapshotWorkItems(ctx, tc, spawnable)
			return nil
		}},
		// gate_parallel_cap: aggregate ceiling across ALL lanes (#1169).
		// Pulled BEFORE gate_cost_cap / gate_cost / gate_l4 so the gate
		// chain only pays per-candidate work for at most ParallelCap
		// items, not the full queue depth (#1172 tick.slow root cause).
		// Disabled when ParallelCap <= 0; lane-cap-only semantics
		// preserved byte-equal for currently-shipped configs.
		{"gate_parallel_cap", func() error {
			if s.cfg.ParallelCap <= 0 {
				return nil
			}
			occ, e := s.db.CountAgentsByLane(ctx, activeStates...)
			if e != nil {
				return fmt.Errorf("scheduler: count agents by lane: %w", e)
			}
			occupancy = occ
			running := 0
			for _, n := range occ {
				running += n
			}
			budget := s.cfg.ParallelCap - running
			if budget <= 0 {
				s.log.Info("scheduler.parallel_cap_saturated",
					"running", running,
					"cap", s.cfg.ParallelCap,
					"queued", len(spawnable),
				)
				spawnable = nil
				return nil
			}
			if len(spawnable) > budget {
				s.log.Info("scheduler.parallel_cap_truncated",
					"running", running,
					"cap", s.cfg.ParallelCap,
					"queued", len(spawnable),
					"kept", budget,
				)
				spawnable = spawnable[:budget]
			}
			return nil
		}},
		// Run cost-cap BEFORE per-scope evaluation: a saturated 24h
		// budget halts the whole tick and saves the approval/cost/l4
		// passes their per-candidate work (spec PHASE-AUTONOMY W5 §5).
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
		// gate_cost before l4: never pay model tokens for cost-denied wi.
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
			// Reuse occupancy from gate_parallel_cap when it ran;
			// avoids double CountAgentsByLane on hot tick.
			if occupancy == nil {
				occ, e := s.db.CountAgentsByLane(ctx, activeStates...)
				if e != nil {
					return e
				}
				occupancy = occ
			}
			r, att, e := s.reserveFromSpawnable(ctx, tc, spawnable, occupancy)
			reserved = r
			attempted = att
			// #1218: when L0 surfaced spawnable rows but reserve produced
			// zero agents, lane caps, parallel cap, or hotspot locks
			// blocked dispatch this tick. Surface at INFO so the operator
			// log surface carries the starved signal even when
			// tickLogLevel masks tick.completed at DEBUG.
			if l0Count > 0 && len(reserved) == 0 {
				s.log.Info(string(obs.EventSchedulerTickStarved),
					string(obs.KeyWorkItemsEvaluated), int64(l0Count),
					string(obs.KeyAgentsReserved), int64(0),
					string(obs.KeyReason), "lane_saturated",
				)
			}
			return e
		}},
		// persist: orphan re-reservation (spec §3.2 step 0.9). Append
		// partial progress before returning err so an orphan-pass halt
		// does not discard earlier reservations (#703 R4).
		{"persist", func() error {
			rest, e := s.reserveOrphans(ctx, tc, occupancy, attempted)
			reserved = append(reserved, rest...)
			if s.backoff != nil {
				s.backoff.Tick()
			}
			return e
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

// tickCtx threads the per-tick WriteHook counter through substrate
// writes; heap-once cost is negligible vs sqlite-tx overhead.
// laneCaps is a snapshot of s.cfg.LaneCaps taken at Tick start so a
// concurrent config mutation cannot oversubscribe a lane mid-tick
// (R31-I5, #1362). workItems is the tick-scoped WorkItem snapshot
// (id → WorkItem) populated by snapshotWorkItems so orphan re-check +
// file-scope collision look-ups do not issue per-agent GetWorkItem
// round-trips (R31-I5, #1359). nil = snapshot not yet built or the
// underlying DB does not implement the batch seam; callers fall back
// to per-id GetWorkItem.
type tickCtx struct {
	writeIndex int
	laneCaps   map[string]int
	workItems  map[string]state.WorkItem
}

// writeHookErr wraps a WriteHook return so per-item err swallow inside
// reserveFromSpawnable can errors.Is-detect the crash-sim path.
// Production WriteHook=nil never constructs this.
type writeHookErr struct{ inner error }

func (e *writeHookErr) Error() string { return "scheduler: write-hook aborted: " + e.inner.Error() }
func (e *writeHookErr) Unwrap() error { return e.inner }

// fireWriteHook is the test-only crash-injection seam (spec §3.3).
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

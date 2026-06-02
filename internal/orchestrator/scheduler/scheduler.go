// Package scheduler picks pending agents that are eligible to spawn
// and reserves their lanes + hotspot locks.
//
// One Tick is a single pass over the queue: it does not block, sleep,
// or call out to the spawner. The orchestrator's Run loop calls Tick
// on a timer and feeds the returned agent IDs into the spawner.
//
// Deadlock safety: hotspot locks are always acquired in lexicographic
// order (docs/design.md §Concurrency & soft-lock policy). Lane
// concurrency is gated by CountAgentsByLane against the configured
// per-lane cap.
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// CostGate is the scheduler-side seam to the cost-governor pre-call
// deny primitive. Production wires *gate.Gate (spec §3.2 concrete type,
// no interface — the gate package itself stays interface-free; this
// scheduler-local seam exists only so tests can inject fakes without
// pulling in spend.Reader + substrate fixtures).
type CostGate interface {
	Evaluate(ctx context.Context, scope gate.WorkItemScope) (gate.Verdict, error)
}

// CostGateResolver maps a planned work_item to its cost scope. Returns
// (_, false) when the wi is out-of-scope (e.g. lane outside the cost
// regime). Mirrors GateResolver shape so the scheduler treats both
// gate-passes uniformly.
type CostGateResolver func(wi state.WorkItem) (gate.WorkItemScope, bool)

// DowngradeHook lets the production wiring surface a soft-cap-induced
// model swap to the spawner. Scheduler-side we record the intent;
// spawner-side Request.ModelOverride consumption lands in Wave 2. Nil
// is legal — soft-cap downgrades degrade to WARN-only.
type DowngradeHook func(workItemID, model string)

// EdgeEvaluator is the seam between Scheduler.Tick and the CEL-based
// predicate evaluator that lives in package program. Defining it here
// (rather than importing program directly) is load-bearing: package
// program already imports package orchestrator for the predicate
// sentinels, and orchestrator imports scheduler — a transitive
// program import would close the cycle.
//
// Schema is opaque (any) at this seam because the runtime evaluator
// does not consult the schema: type-checking against the upstream's
// OutputsSchema happens at brief-load time (ValidateV2 in program).
// Production wires program.EdgeEvaluator; tests inject fakes.
type EdgeEvaluator interface {
	Eval(ctx context.Context, edge state.EdgeRow, schema any, journal state.OutputJournalEntry) (bool, string, error)
}

// edge_fired_* string sentinels mirror the work_item_edges.fired
// column's text-enum values. Kept package-local because the values
// are also referenced verbatim by SQL filters in state.work_item_edges
// (sqlite indexed equality on 'pending').
const (
	edgeFiredTrue    = "true"
	edgeFiredFalse   = "false"
	edgeFiredPending = "pending"
)

// OutputsSchemaResolver maps an upstream feature ID to the live
// OutputsSchema declared by the brief. Returns (nil, false) when the
// feature is unknown or the brief has not yet been loaded — the
// scheduler treats that as "evaluate with nil schema", matching the
// runtime evaluator's contract.
//
// Production wiring (W5) backs this with the BriefLoader's in-memory
// program → []PlannedFeatureV2 map, invalidated on each Sync. MVP-2
// W4 ships only the seam: tests pass closures, the orchestrator
// constructor passes nil (runtime evaluator ignores schema, so this
// is harmless until W5 lights up).
type OutputsSchemaResolver func(featureID string) (any, bool)

// HotspotResolver maps a work-item ID to the hotspot lock names the
// item touches. The orchestrator typically derives this from
// regatta.yaml's `hotspots` list + per-lane `paths`. An empty slice is
// a valid return for items that touch no hotspots.
type HotspotResolver func(workItemID string) []string

// ApprovalGate is the scheduler-side seam to the HITL gate. Production
// wires *approval.Gate (constructed in serve.go); tests inject fakes.
// The seam keeps scheduler ↔ approval coupling to a single method so
// the gate's transitive dependencies (canon.Keyring, notifier, slog)
// stay invisible to scheduler tests.
type ApprovalGate interface {
	Evaluate(ctx context.Context, wi state.WorkItem, cfg approval.Config) (approval.Result, error)
}

// GateResolver maps a planned work_item to the approval gate config
// that governs it. Returns (_, false) when the wi is non-gated — the
// scheduler then treats it as a plain spawnable. Production wiring
// closes over a regatta.yaml-derived gate map; the resolution policy
// (by-lane, by-tag, predicate-CEL) lives in serve.go so the scheduler
// stays policy-agnostic.
type GateResolver func(wi state.WorkItem) (approval.Config, bool)

// Config holds tunables for a Scheduler. All durations and caps are
// derived from regatta.yaml at orchestrator construction time.
type Config struct {
	// LaneCaps gives the max number of active agents per lane. A lane
	// missing from the map is treated as unlimited. The default lane
	// uses the empty-string key.
	LaneCaps map[string]int

	// LockTTL is the heartbeat lease duration passed to
	// state.DB.TryAcquireLocks. A lock whose heartbeat is older than
	// LockTTL is considered released and may be stolen.
	LockTTL time.Duration

	// Hotspots resolves an item to its hotspot lock names. If nil, the
	// scheduler skips lock acquisition entirely.
	Hotspots HotspotResolver

	// Evaluator runs CEL predicates over journaled outputs. nil
	// disables edge eval; the scheduler then behaves like its MVP-1
	// self (depends_on_features only). Production wires
	// program.NewEdgeEvaluator() — instance is shared across ticks
	// so its compile cache survives.
	Evaluator EdgeEvaluator

	// OutputsSchemas resolves an upstream feature's OutputsSchema for
	// runtime predicate eval. nil is legal: the runtime evaluator does
	// not consult schema, so the field is reserved for forward-compat
	// (W5 plumbs BriefLoader-backed lookup).
	OutputsSchemas OutputsSchemaResolver

	// Logger is the structured-event sink for scheduler edge eval and
	// reservation skips. Nil falls back to slog.Default() so embedded
	// callers still get output without panicking (spec §4.1).
	Logger *slog.Logger

	// Gate evaluates the HITL approval gate-pass per spec §3.1 step 0.5.
	// Nil disables the gate-pass entirely so the scheduler behaves like
	// its pre-Wave-3 self for callers that have not yet wired approval
	// gates.
	Gate ApprovalGate

	// GateResolver maps a planned work_item to its gate config (or
	// (_, false) when non-gated). Nil disables the gate-pass; same
	// semantics as Gate=nil so an operator who forgets one of the two
	// gets a clean "gates disabled" rather than a half-wired runtime.
	GateResolver GateResolver

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("scheduler") which resolves to the
	// global provider — noop until obs/otel.Setup runs. Per W6 spec
	// §3.3 + feedback_spec_pattern_authority.
	Tracer trace.Tracer

	// CostGate evaluates the cost-governor pre-call deny gate per spec
	// §3.2 step 0.6. Nil disables the cost-pass — applyCostGovernor
	// short-circuits to identity (zero overhead per spec §8 row 1 + I6).
	CostGate CostGate

	// CostGateResolver maps a planned work_item to its cost scope.
	// Nil disables the cost-pass; same semantics as CostGate=nil.
	CostGateResolver CostGateResolver

	// OnDowngrade is invoked once per Evaluate that returns
	// SoftCapBreached=true with a DowngradeTo target AND a scope that
	// opted in via AllowDowngrade. Wave 1 surfaces the intent; the
	// spawner-side ModelOverride consumer lands in Wave 2.
	OnDowngrade DowngradeHook
}

// schedulerDB is the seam between Scheduler and state.DB. The
// production constructor accepts *state.DB (which satisfies this
// interface by method set), and crash-injection tests wrap the real
// DB to drop specific writes mid-tick — see Seam-2 in spec
// docs/superpowers/specs/2026-05-31-fix-98-scheduler-default-fallback.md
// §5.1. No production code path treats db as anything other than the
// real *state.DB.
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
	// SQL escapes the seam for the gate-pass's raw-SQL CAS write that
	// transitions a rejected work_item to status='rejected'. Spec §3.1
	// step 0.5 spells this as state.TransitionWorkItem, but state has no
	// typed transition for work_items yet; the precedent from
	// internal/program/brief_loader.go (the archive-on-archived-dep path)
	// is to use db.SQL() directly. Promoting this to a typed method on
	// *state.DB is tracked as a followup so the next wave can consolidate.
	SQL() *sql.DB
}

// Scheduler is single-caller: Tick must not be invoked concurrently.
// The orchestrator's Run loop owns the only call site and is itself
// flock-guarded by PollOnce, so this contract holds in practice
// without an explicit mutex. Admin commands that want to peek at
// state must use the read-only state.DB queries directly rather than
// calling Tick.
type Scheduler struct {
	db     schedulerDB
	cfg    Config
	log    *slog.Logger
	tracer trace.Tracer

	// multiDefaultLogged dedupes the edge.multiple_defaults_per_from
	// warn so a misconfigured brief logs once per (program_id, from_id)
	// per scheduler-process lifetime instead of every tick.
	multiDefaultLogged sync.Map

	// WriteHook is test-only; see
	// docs/engineer/specs/2026-06-02-s3-t4-crash-recovery-property.md §3.3.
	// Fires before every substrate-bound write inside Tick with a
	// monotonically-incrementing writeIndex. Returning non-nil aborts
	// Tick mid-flight, simulating a crash at write index k.
	// Production wires nil — the field stays out of the hot path.
	WriteHook func(writeIndex int) error
}

// New constructs a Scheduler. The Config is copied; later mutations to
// cfg.LaneCaps do not affect the running scheduler.
func New(db *state.DB, cfg Config) *Scheduler {
	return newScheduler(db, cfg)
}

// newWithDB is a test-only constructor that accepts the schedulerDB
// interface so crash-injection tests can wrap *state.DB. The exported
// New keeps its *state.DB signature; production callers are unchanged.
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
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("scheduler")
	}
	return &Scheduler{db: db, cfg: cfg, log: log, tracer: tracer}
}

// activeStates lists agent states that count against a lane's
// concurrency cap. Pending agents do not count (they are eligible
// candidates, not active workers); terminal states never count.
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
// applies the HITL approval gate-pass, then reserves lanes + hotspot
// locks for each surviving work_item.
//
// Lock acquire is non-blocking; an agent whose hotspot is held by
// another agent is left in pending and retried next tick.
//
// Step-0 (when cfg.Evaluator != nil) evaluates every still-pending
// edge whose from_id is in status=merged. Per-edge errors warn on
// the injected logger and continue so one bad predicate cannot stall
// the rest of the tick (spec §3.9). The eval pass writes to
// work_item_edges only — ListSpawnable observes the updated fired
// column on the next reservation pass.
//
// Per-agent reservation runs as a single sqlite tx — UpsertPending +
// TryAcquireLocks + TransitionAgent commit atomically or roll back as
// a unit (issue #88). A crash mid-reservation leaves no orphan lock
// rows and no agent stranded in spawning without locks; orphan-row
// safety is preserved via two complementary mechanisms:
//   - ListSpawnable filters `a.id IS NULL`, so once a work_item has
//     any agent row the spawnable query stops re-emitting it.
//   - ListAgentsByState(AgentPending) re-discovers any agent that
//     was upserted as pending but never transitioned (lane-capped
//     last tick, hotspot-blocked, or a future failure mode), letting
//     the next tick retry the reservation tx.
func (s *Scheduler) Tick(ctx context.Context) ([]int64, error) {
	tc := &tickCtx{}
	if s.cfg.Evaluator != nil {
		if err := s.evalPendingEdges(ctx, tc); err != nil {
			return nil, fmt.Errorf("scheduler: eval edges: %w", err)
		}
	}
	if _, err := s.db.ExpireStaleLocks(ctx, s.cfg.LockTTL); err != nil {
		return nil, fmt.Errorf("scheduler: expire stale locks: %w", err)
	}

	spawnable, err := s.db.ListSpawnable(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list spawnable: %w", err)
	}
	// Spec §3.1 step 0.5 — HITL gate-pass between edge-eval and reserve.
	// Filters spawnable in-place: Proceed keeps the wi, Pause drops it
	// from this tick (re-evaluated next tick because status stays
	// planned), Reject flips status to 'rejected' so future ticks no
	// longer surface it via ListSpawnable.
	spawnable, err = s.applyApprovalGates(ctx, tc, spawnable)
	if err != nil {
		return nil, fmt.Errorf("scheduler: apply approval gates: %w", err)
	}
	// Spec §3.2 step 0.6 — cost-governor pre-call deny. Slots in
	// between approval-gate and reserve, BEFORE laneHasCapacity is
	// consulted. Identity short-circuit when CostGate is nil.
	spawnable, err = s.applyCostGovernor(ctx, spawnable)
	if err != nil {
		return nil, fmt.Errorf("scheduler: apply cost governor: %w", err)
	}

	occupancy, err := s.db.CountAgentsByLane(ctx, activeStates...)
	if err != nil {
		return nil, err
	}

	reserved, attempted, err := s.reserveFromSpawnable(ctx, tc, spawnable, occupancy)
	if err != nil {
		return reserved, err
	}

	rest, err := s.reserveOrphans(ctx, tc, occupancy, attempted)
	if err != nil {
		return reserved, err
	}
	return append(reserved, rest...), nil
}

// tickCtx threads the per-tick WriteHook counter through Tick's
// substrate-bound write sites. Heap-once-per-tick allocation cost is
// negligible vs. sqlite-tx overhead; pointer keeps the counter shared
// across the helpers reserveFromSpawnable, reserveOrphans, etc.
type tickCtx struct{ writeIndex int }

// writeHookErr wraps a non-nil WriteHook return so the per-item err
// swallow inside reserveFromSpawnable can errors.Is-detect the
// crash-sim path and propagate it up to Tick's caller. Production
// WriteHook=nil never constructs this.
type writeHookErr struct{ inner error }

func (e *writeHookErr) Error() string { return "scheduler: write-hook aborted: " + e.inner.Error() }
func (e *writeHookErr) Unwrap() error { return e.inner }

// fireWriteHook is the test-only crash-injection seam (spec §3.3). Nil
// hook short-circuits the production path with one branch + one
// pointer-deref, no allocs. Each call increments writeIndex even when
// the hook is nil so the counter's meaning is independent of test
// wiring.
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

// reserveFromSpawnable walks the gate-pass-filtered spawnable slice
// and, for each ready work_item, runs a single tx that upserts the
// pending agent + acquires hotspot locks + transitions to spawning.
// Lane-capped items still get their pending row materialized
// (committed in a no-transition tx) so the next tick's reserveOrphans
// pass picks them up once capacity frees.
func (s *Scheduler) reserveFromSpawnable(ctx context.Context, tc *tickCtx, spawnable []state.WorkItem, occupancy map[string]int) (reserved []int64, attempted map[int64]struct{}, err error) {
	attempted = map[int64]struct{}{}
	var failures int
	for _, w := range spawnable {
		if err := ctx.Err(); err != nil {
			return reserved, attempted, err
		}
		// W6 spec §3.5: one `work_item` span per work_item lifecycle
		// under the active `tick` span. Attrs match spec §4.1.
		itemCtx, itemSpan := s.tracer.Start(ctx, "work_item",
			trace.WithAttributes(
				attribute.String(string(obs.KeyWorkItemID), w.ID),
				attribute.String(string(obs.KeyLane), w.Lane),
				attribute.String("regatta.kind", string(w.Kind)),
			))
		hasCap := s.laneHasCapacity(w.Lane, occupancy)
		agentID, transitioned, err := s.reserveOne(itemCtx, tc, w.ID, w.Lane, hasCap)
		itemSpan.End()
		if err != nil {
			// WriteHook crash-sim error MUST abort the tick (spec §3.3) —
			// bypass the per-item swallow that protects against production
			// row-shape errors.
			var hookErr *writeHookErr
			if errors.As(err, &hookErr) {
				return reserved, attempted, err
			}
			if errors.Is(err, state.ErrLockHeld) {
				// The full reservation tx rolled back, so the pending
				// row is not yet materialized for this work_item.
				// Re-upsert without locks so the row persists for the
				// next tick (matching pre-refactor materialize behavior)
				// — and the log's agent_id is a real persistent ID
				// rather than the rolled-back rowid. Mark the agent as
				// already-attempted so reserveOrphans does not re-try
				// the same lock acquisition this same tick.
				if hookErr := s.fireWriteHook(tc); hookErr != nil {
					return reserved, attempted, hookErr
				}
				if a, upErr := s.db.UpsertPending(ctx, w.ID, w.Lane); upErr == nil {
					agentID = a.ID
					attempted[a.ID] = struct{}{}
				} else {
					s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
						string(obs.KeyWorkItemID), w.ID,
						string(obs.KeyReason), "upsert_after_lock_held_failed",
						string(obs.KeyErr), upErr.Error(),
					)
				}
				s.log.Info("scheduler.agent_skipped_hotspot_locked",
					string(obs.KeyAgentID), agentID,
					string(obs.KeyWorkItemID), w.ID,
					string(obs.KeyReason), "hotspot_locked",
				)
				continue
			}
			// Per-item failure: log and skip rather than abort the
			// batch so one bad row cannot stall the queue.
			failures++
			s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
				string(obs.KeyWorkItemID), w.ID,
				string(obs.KeyReason), "reserve_failed",
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		if agentID != 0 {
			attempted[agentID] = struct{}{}
		}
		if transitioned {
			occupancy[w.Lane]++
			reserved = append(reserved, agentID)
		}
	}
	if failures > 0 {
		s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
			string(obs.KeyReason), "pass_completed_with_failures",
			"failures", failures,
			"total", len(spawnable),
		)
	}
	return reserved, attempted, nil
}

// reserveOrphans rediscovers AgentPending rows that reserveFromSpawnable
// did not transition this tick — typically lane-capped items from a
// prior tick, or any future class of pending agents (e.g. crashed
// recovery requeue). Each gets a single-tx reservation (locks +
// transition). The agent row already exists, so the tx omits the
// upsert step.
func (s *Scheduler) reserveOrphans(ctx context.Context, tc *tickCtx, occupancy map[string]int, attempted map[int64]struct{}) ([]int64, error) {
	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list pending: %w", err)
	}
	var reserved []int64
	for _, a := range pending {
		if _, seen := attempted[a.ID]; seen {
			// reserveFromSpawnable already tried this agent this tick
			// (lane-capped or lock-held). Skip the duplicate attempt
			// so logs and lock-acquire churn stay one-per-agent-per-tick.
			continue
		}
		if !s.laneHasCapacity(a.Lane, occupancy) {
			continue
		}
		locks := s.resolveLocks(a.WorkItemID)
		err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
			if err := s.fireWriteHook(tc); err != nil {
				return err
			}
			if err := s.db.TryAcquireLocksTx(ctx, tx, locks, a.ID, s.cfg.LockTTL); err != nil {
				return err
			}
			if err := s.fireWriteHook(tc); err != nil {
				return err
			}
			_, err := s.db.TransitionAgentTx(ctx, tx, a.ID, state.AgentSpawning, state.AgentMutation{})
			return err
		})
		if err != nil {
			if errors.Is(err, state.ErrLockHeld) {
				s.log.Info("scheduler.agent_skipped_hotspot_locked",
					string(obs.KeyAgentID), a.ID,
					string(obs.KeyWorkItemID), a.WorkItemID,
					string(obs.KeyReason), "hotspot_locked",
				)
				continue
			}
			return reserved, fmt.Errorf("scheduler: reserve orphan agent %d: %w", a.ID, err)
		}
		occupancy[a.Lane]++
		reserved = append(reserved, a.ID)
	}
	return reserved, nil
}

// reserveOne wraps the per-work-item reservation in a single tx.
// When hasCap is false the tx commits the upsert only — the row stays
// pending and the next tick's reserveOrphans retries the
// locks+transition portion. Returns transitioned=true only when the
// agent left pending for spawning inside this tx.
func (s *Scheduler) reserveOne(ctx context.Context, tc *tickCtx, workItemID, lane string, hasCap bool) (agentID int64, transitioned bool, err error) {
	locks := s.resolveLocks(workItemID)
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if hookErr := s.fireWriteHook(tc); hookErr != nil {
			return hookErr
		}
		a, upErr := s.db.UpsertPendingTx(ctx, tx, workItemID, lane)
		if upErr != nil {
			return upErr
		}
		agentID = a.ID
		if !hasCap {
			return nil
		}
		if hookErr := s.fireWriteHook(tc); hookErr != nil {
			return hookErr
		}
		if lockErr := s.db.TryAcquireLocksTx(ctx, tx, locks, a.ID, s.cfg.LockTTL); lockErr != nil {
			return lockErr
		}
		if hookErr := s.fireWriteHook(tc); hookErr != nil {
			return hookErr
		}
		if _, trErr := s.db.TransitionAgentTx(ctx, tx, a.ID, state.AgentSpawning, state.AgentMutation{}); trErr != nil {
			return trErr
		}
		transitioned = true
		return nil
	})
	return agentID, transitioned, err
}

// applyApprovalGates filters spawnable through the HITL gate-pass per
// spec §3.1 step 0.5. Returns the surviving slice: Proceed keeps the
// wi for the reservation loop, Pause drops it from this tick, Reject
// drops it AND flips its status to 'rejected' so future ListSpawnable
// calls no longer surface it.
//
// Disabled (Gate or GateResolver nil) returns spawnable unchanged so
// callers that have not wired approval gates pay zero cost.
//
// Per-wi evaluation errors warn on the injected logger and treat the
// wi as paused for this tick — same fail-closed posture spec §3.2 uses
// for journal-load failures: a misconfigured gate must not stall the
// rest of the tick, but the wi MUST NOT advance until the gate is
// healthy. Reject-transition failures DO halt the tick because a wi
// stuck in 'planned' after a reject verdict would silently retry until
// the operator notices.
func (s *Scheduler) applyApprovalGates(ctx context.Context, tc *tickCtx, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.Gate == nil || s.cfg.GateResolver == nil {
		return spawnable, nil
	}
	kept := make([]state.WorkItem, 0, len(spawnable))
	for _, wi := range spawnable {
		cfg, gated := s.cfg.GateResolver(wi)
		if !gated {
			kept = append(kept, wi)
			continue
		}
		// W6 spec §3.5: gate.evaluate must be a child of the active
		// work_item span. Open the work_item span here for the gated
		// branch so Gate.Evaluate sees the parent ctx.
		itemCtx, itemSpan := s.tracer.Start(ctx, "work_item",
			trace.WithAttributes(
				attribute.String(string(obs.KeyWorkItemID), wi.ID),
				attribute.String(string(obs.KeyLane), wi.Lane),
				attribute.String("regatta.kind", string(wi.Kind)),
			))
		res, err := s.cfg.Gate.Evaluate(itemCtx, wi, cfg)
		itemSpan.End()
		if err != nil {
			s.log.Warn(string(obs.EventApprovalDecided),
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyGateID), cfg.Name,
				string(obs.KeyVerdict), approval.ResultPause.String(),
				string(obs.KeyReason), "evaluate_error",
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		switch res {
		case approval.ResultProceed:
			kept = append(kept, wi)
		case approval.ResultReject:
			if err := s.markWorkItemRejected(ctx, tc, wi.ID); err != nil {
				return nil, fmt.Errorf("mark %s rejected: %w", wi.ID, err)
			}
			s.log.Info(string(obs.EventApprovalDecided),
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyGateID), cfg.Name,
				string(obs.KeyVerdict), approval.ResultReject.String(),
			)
		default:
			// ResultPause (zero value) — drop from this tick. The approval
			// row persists; next Tick re-Evaluates and observes the same
			// fold-of-events until a reviewer decides or the reaper times
			// it out (spec §3.3).
			s.log.Info(string(obs.EventApprovalDecided),
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyGateID), cfg.Name,
				string(obs.KeyVerdict), approval.ResultPause.String(),
			)
		}
	}
	return kept, nil
}

// applyCostGovernor filters spawnable through the cost-governor pre-call
// deny gate per spec §3.2 step 0.6. Returns the surviving slice:
// Allow=true keeps the wi for the reservation loop, Allow=false drops
// the wi from this tick (status stays 'planned' so the next tick
// re-evaluates after the reconciler may have caught up).
//
// Disabled (CostGate or CostGateResolver nil) returns spawnable
// byte-equal so callers that have not wired the cost-governor pay
// ZERO overhead (closes I6 per spec §8 row 1).
//
// Per-wi evaluation errors warn on the injected logger and treat the
// wi as denied for this tick — fail-closed posture matches the
// approval gate's: a misconfigured gate must not silently allow.
//
// Soft-cap downgrade (spec R10): when Verdict.SoftCapBreached AND the
// wi opted-in via scope.AllowDowngrade, OnDowngrade fires with the
// suggested model so the spawner's Request.ModelOverride can carry
// the swap (Wave 2 spawner consumer).
func (s *Scheduler) applyCostGovernor(ctx context.Context, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.CostGate == nil || s.cfg.CostGateResolver == nil {
		return spawnable, nil
	}
	kept := make([]state.WorkItem, 0, len(spawnable))
	for _, wi := range spawnable {
		scope, ok := s.cfg.CostGateResolver(wi)
		if !ok {
			kept = append(kept, wi)
			continue
		}
		v, err := s.cfg.CostGate.Evaluate(ctx, scope)
		if err != nil {
			s.log.Warn("scheduler.cost_gate_error",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		if !v.Allow {
			s.log.Info("scheduler.cost_gate_denied",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyReason), v.Reason,
			)
			continue
		}
		if v.SoftCapBreached && v.DowngradeTo != "" && s.cfg.OnDowngrade != nil {
			s.cfg.OnDowngrade(wi.ID, v.DowngradeTo)
		}
		kept = append(kept, wi)
	}
	return kept, nil
}

// markWorkItemRejected flips work_items.status='rejected' atomically
// against the planned-source state. CAS predicate (`status='planned'`)
// is load-bearing: a concurrent writer that already moved the row
// (cancel/archive) must win — the gate cannot resurrect a terminal wi.
//
// Spec §3.1 calls this state.TransitionWorkItem; state has no typed
// work_item transition yet, so the raw-SQL pattern matches the
// brief_loader.go archive-on-archived-dep precedent. A followup will
// consolidate both into a state-package API.
func (s *Scheduler) markWorkItemRejected(ctx context.Context, tc *tickCtx, id string) error {
	if err := s.fireWriteHook(tc); err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.SQL().ExecContext(ctx,
		`UPDATE work_items SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		"rejected", now, id, string(state.WorkStatusPlanned))
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("scheduler: work_item %s no longer in status=planned", id)
	}
	return nil
}

func (s *Scheduler) laneHasCapacity(lane string, occupancy map[string]int) bool {
	limit, gated := s.cfg.LaneCaps[lane]
	if !gated {
		return true
	}
	return occupancy[lane] < limit
}

// evalPendingEdges runs Tick step-0: for each (program, from_id)
// group of pending edges whose source is merged, fetch the latest
// journal entry and evaluate every outgoing edge. The result writes
// back via MarkEdgeFired with the journal's content_sha so a later
// replay can reproduce the routing decision (spec §3.8 + §3.9).
//
// Per-edge eval errors warn on the injected logger and mark the
// edge fired='false' rather than halting the tick: a single bad
// predicate is a config bug operators see in logs, not a
// stop-the-world fault. Missing journal (ErrJournalNotFound) is
// logged at info and leaves the edge pending so the next tick
// retries once the spawner catches up.
//
// Default-next fallback (spec §3.3 rule 2c): when every non-default
// outbound edge from a from_id resolves to fired='false', the
// default_next edge is flipped to fired='true' with the same
// journal sha. BriefLoader validation (W2-A CheckReachability) has
// already pinned the default's target as a live feature, so the
// fallback never strands flow.
func (s *Scheduler) evalPendingEdges(ctx context.Context, tc *tickCtx) error {
	edges, err := s.db.ListPendingEdgesFromMerged(ctx)
	if err != nil {
		return fmt.Errorf("list pending edges: %w", err)
	}
	byFrom := map[string][]state.EdgeRow{}
	order := make([]string, 0)
	for _, e := range edges {
		if _, seen := byFrom[e.FromID]; !seen {
			order = append(order, e.FromID)
		}
		byFrom[e.FromID] = append(byFrom[e.FromID], e)
	}
	for _, fromID := range order {
		group := byFrom[fromID]
		journal, err := s.db.GetLatestOutput(ctx, fromID)
		if err != nil {
			if errors.Is(err, state.ErrJournalNotFound) {
				s.log.Info(string(obs.EventEdgeEvalSkippedNoJournal),
					string(obs.KeyFromID), fromID,
				)
				continue
			}
			s.log.Warn(string(obs.EventEdgeJournalLoadFailed),
				string(obs.KeyFromID), fromID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		schema, _ := s.resolveSchema(fromID)
		for _, e := range group {
			if e.IsDefault {
				continue
			}
			fired, reason, evalErr := s.cfg.Evaluator.Eval(ctx, e, schema, journal)
			if evalErr != nil {
				s.log.Warn(string(obs.EventEdgeEvalError),
					string(obs.KeyEdgeID), e.ID,
					string(obs.KeyProgramID), e.ProgramID,
					string(obs.KeyFromID), e.FromID,
					string(obs.KeyToID), e.ToID,
					string(obs.KeyErr), evalErr.Error(),
				)
				fired = false
				reason = "eval-error"
			}
			firedStr := edgeFiredFalse
			if fired {
				firedStr = edgeFiredTrue
			}
			if err := s.fireWriteHook(tc); err != nil {
				return err
			}
			if err := s.db.MarkEdgeFired(ctx, e.ID, firedStr, journal.ContentSHA); err != nil {
				s.log.Warn(string(obs.EventEdgeMarkFailed),
					string(obs.KeyEdgeID), e.ID,
					string(obs.KeyErr), err.Error(),
				)
				continue
			}
			evt := obs.EventEdgeFired
			if !fired {
				evt = obs.EventEdgeSkipped
			}
			s.log.Info(string(evt),
				string(obs.KeyEdgeID), e.ID,
				string(obs.KeyProgramID), e.ProgramID,
				string(obs.KeyFromID), e.FromID,
				string(obs.KeyToID), e.ToID,
				"predicate", e.PredicateCEL,
				string(obs.KeyReason), reason,
				string(obs.KeyJournalSHA), journal.ContentSHA,
			)
		}

		// Aggregate the post-write sibling state in a single index
		// scan instead of re-reading the slice. Equivalent semantic
		// to the prior ListEdgesFrom path (same post-write read
		// under the single-writer flock — state.go:9, orchestrator
		// PollOnce) but without the per-group slice alloc that drove
		// the +19% allocs/op regression in #187. See spec §3.2 for
		// the post-write rationale that rules out a tick-local
		// accumulator.
		agg, err := s.db.CountNonDefaultEdgeStates(ctx, fromID)
		if err != nil {
			s.log.Warn("edge.list_siblings_failed",
				string(obs.KeyFromID), fromID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		if agg.DefaultCount > 1 {
			// Dedupe per (program_id, from_id) so a permanently
			// misconfigured brief does not spam every tick.
			key := agg.DefaultProgramID + "\x00" + fromID
			if _, loaded := s.multiDefaultLogged.LoadOrStore(key, struct{}{}); !loaded {
				s.log.Warn(string(obs.EventEdgeMultipleDefaultsPerFrom),
					string(obs.KeyProgramID), agg.DefaultProgramID,
					string(obs.KeyFromID), fromID,
					"count", agg.DefaultCount,
				)
			}
		}
		if agg.DefaultCount == 0 || agg.DefaultFired != edgeFiredPending {
			continue
		}
		if agg.AnyNonDefaultTrue || agg.AnyNonDefaultPending || agg.NonDefaultCount == 0 {
			continue
		}
		if err := s.fireWriteHook(tc); err != nil {
			return err
		}
		if err := s.db.MarkEdgeFired(ctx, agg.DefaultEdgeID, edgeFiredTrue, journal.ContentSHA); err != nil {
			s.log.Warn("edge.default_mark_failed",
				string(obs.KeyEdgeID), agg.DefaultEdgeID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		s.log.Info(string(obs.EventEdgeDefaultFallback),
			string(obs.KeyEdgeID), agg.DefaultEdgeID,
			string(obs.KeyFromID), fromID,
			string(obs.KeyJournalSHA), journal.ContentSHA,
		)
	}
	return nil
}

func (s *Scheduler) resolveSchema(featureID string) (any, bool) {
	if s.cfg.OutputsSchemas == nil {
		return nil, false
	}
	return s.cfg.OutputsSchemas(featureID)
}

func (s *Scheduler) resolveLocks(workItemID string) []string {
	if s.cfg.Hotspots == nil {
		return nil
	}
	names := s.cfg.Hotspots(workItemID)
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	sort.Strings(out)
	return out
}

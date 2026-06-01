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

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

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
	ListAgentsByState(ctx context.Context, states ...state.AgentState) ([]state.Agent, error)
	CountAgentsByLane(ctx context.Context, states ...state.AgentState) (map[string]int, error)
	TransitionAgent(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error)
	TryAcquireLocks(ctx context.Context, names []string, agentID int64, ttl time.Duration) error
	ReleaseAgentLocks(ctx context.Context, agentID int64) (int64, error)
	ExpireStaleLocks(ctx context.Context, ttl time.Duration) (int64, error)
	ListPendingEdgesFromMerged(ctx context.Context) ([]state.EdgeRow, error)
	ListEdgesFrom(ctx context.Context, fromID string) ([]state.EdgeRow, error)
	MarkEdgeFired(ctx context.Context, edgeID int64, fired, contentSHA string) error
	GetLatestOutput(ctx context.Context, workItemID string) (state.OutputJournalEntry, error)
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
	db  schedulerDB
	cfg Config
	log *slog.Logger

	// multiDefaultLogged dedupes the edge.multiple_defaults_per_from
	// warn so a misconfigured brief logs once per (program_id, from_id)
	// per scheduler-process lifetime instead of every tick.
	multiDefaultLogged sync.Map
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
	return &Scheduler{db: db, cfg: cfg, log: log}
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
// materializes a pending agents row for any work_item missing one,
// then reserves lanes + hotspot locks as before.
//
// Lock acquire is non-blocking; an agent whose hotspot is held by
// another agent is left in pending and retried next tick.
//
// Step-0 (when cfg.Evaluator != nil) evaluates every still-pending
// edge whose from_id is in status=merged. Per-edge errors warn on
// the injected logger and continue so one bad predicate cannot stall
// the rest of the tick (spec §3.9). The eval pass writes to
// work_item_edges only — ListSpawnable observes the updated fired
// column on the next materializePending call.
func (s *Scheduler) Tick(ctx context.Context) ([]int64, error) {
	if s.cfg.Evaluator != nil {
		if err := s.evalPendingEdges(ctx); err != nil {
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
	spawnable, err = s.applyApprovalGates(ctx, spawnable)
	if err != nil {
		return nil, fmt.Errorf("scheduler: apply approval gates: %w", err)
	}

	pending, err := s.materializePending(ctx, spawnable)
	if err != nil {
		return nil, fmt.Errorf("scheduler: materialize: %w", err)
	}

	occupancy, err := s.db.CountAgentsByLane(ctx, activeStates...)
	if err != nil {
		return nil, err
	}

	// Each reservation below is 3 sequential txs — UpsertPending (in
	// materializePending), TryAcquireLocks, TransitionAgent — not one.
	// Orphan-row safety: an agent row that crashes between UpsertPending
	// and TransitionAgent is still in `pending`, so the next Tick
	// re-discovers it via ListAgentsByState(AgentPending) and retries
	// the lock+transition without re-running ListSpawnable (the
	// `a.id IS NULL` join filter would exclude it). MVP-2 may tighten
	// this to a single tx; current split is intentional to keep the
	// state-package surface small.
	var reserved []int64
	for _, a := range pending {
		if !s.laneHasCapacity(a.Lane, occupancy) {
			continue
		}
		locks := s.resolveLocks(a.WorkItemID)
		if err := s.db.TryAcquireLocks(ctx, locks, a.ID, s.cfg.LockTTL); err != nil {
			if errors.Is(err, state.ErrLockHeld) {
				s.log.Info("scheduler.agent_skipped_hotspot_locked",
					string(obs.KeyAgentID), a.ID,
					string(obs.KeyWorkItemID), a.WorkItemID,
					string(obs.KeyReason), "hotspot_locked",
				)
				continue
			}
			return reserved, fmt.Errorf("scheduler: acquire locks for agent %d: %w", a.ID, err)
		}
		if _, err := s.db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
			// Release the locks we just took so we don't deadlock the
			// next tick.
			if _, releaseErr := s.db.ReleaseAgentLocks(ctx, a.ID); releaseErr != nil {
				s.log.Warn("scheduler.release_locks_after_transition_failure",
					string(obs.KeyAgentID), a.ID,
					string(obs.KeyErr), releaseErr.Error(),
				)
			}
			return reserved, fmt.Errorf("scheduler: mark agent %d spawning: %w", a.ID, err)
		}
		occupancy[a.Lane]++
		reserved = append(reserved, a.ID)
	}
	return reserved, nil
}

// materializePending UpsertPending-s one agents row per work_item in
// the supplied spawnable slice (filtered upstream by Tick — the gate-
// pass may have removed paused/rejected wi rows). Returns the union:
// every existing pending agent + every newly-materialized one, in
// db-natural order. Per spec §2.8 — Scheduler is the materialization
// point so the orphan-row class is impossible by construction.
//
// Per-item UpsertPending failures are logged and skipped rather than
// aborting the batch: a single bad row should not stall every other
// pending agent. Context cancellation still propagates.
func (s *Scheduler) materializePending(ctx context.Context, spawnable []state.WorkItem) ([]state.Agent, error) {
	var failures int
	for _, w := range spawnable {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := s.db.UpsertPending(ctx, w.ID, w.Lane); err != nil {
			failures++
			s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
				string(obs.KeyWorkItemID), w.ID,
				string(obs.KeyReason), "upsert_pending_failed",
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
	}
	if failures > 0 {
		s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
			string(obs.KeyReason), "pass_completed_with_failures",
			"failures", failures,
			"total", len(spawnable),
		)
	}
	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list pending: %w", err)
	}
	return pending, nil
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
func (s *Scheduler) applyApprovalGates(ctx context.Context, spawnable []state.WorkItem) ([]state.WorkItem, error) {
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
		res, err := s.cfg.Gate.Evaluate(ctx, wi, cfg)
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
			if err := s.markWorkItemRejected(ctx, wi.ID); err != nil {
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

// markWorkItemRejected flips work_items.status='rejected' atomically
// against the planned-source state. CAS predicate (`status='planned'`)
// is load-bearing: a concurrent writer that already moved the row
// (cancel/archive) must win — the gate cannot resurrect a terminal wi.
//
// Spec §3.1 calls this state.TransitionWorkItem; state has no typed
// work_item transition yet, so the raw-SQL pattern matches the
// brief_loader.go archive-on-archived-dep precedent. A followup will
// consolidate both into a state-package API.
func (s *Scheduler) markWorkItemRejected(ctx context.Context, id string) error {
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
func (s *Scheduler) evalPendingEdges(ctx context.Context) error {
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

		// Read the post-write sibling set from the DB so we see every
		// edge already resolved by prior ticks plus everything this
		// tick just committed. Pre-fix the tick-local accumulator
		// missed siblings resolved before a partial-tick crash; spec
		// §3.2 has the full rationale.
		// Safe under single-writer flock invariant (state.go:9, orchestrator PollOnce).
		siblings, err := s.db.ListEdgesFrom(ctx, fromID)
		if err != nil {
			s.log.Warn("edge.list_siblings_failed",
				string(obs.KeyFromID), fromID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		var (
			defaultRow      *state.EdgeRow
			defaultCount    int
			nonDefaultFired int
			anyTrue         bool
			anyPending      bool
		)
		for i := range siblings {
			e := &siblings[i]
			if e.IsDefault {
				defaultCount++
				defaultRow = e
				continue
			}
			nonDefaultFired++
			switch e.Fired {
			case edgeFiredTrue:
				anyTrue = true
			case edgeFiredPending:
				anyPending = true
			}
		}
		if defaultCount > 1 {
			// Dedupe per (program_id, from_id) so a permanently
			// misconfigured brief does not spam every tick.
			programID := ""
			if len(siblings) > 0 {
				programID = siblings[0].ProgramID
			}
			key := programID + "\x00" + fromID
			if _, loaded := s.multiDefaultLogged.LoadOrStore(key, struct{}{}); !loaded {
				s.log.Warn(string(obs.EventEdgeMultipleDefaultsPerFrom),
					string(obs.KeyProgramID), programID,
					string(obs.KeyFromID), fromID,
					"count", defaultCount,
				)
			}
		}
		if defaultRow == nil || defaultRow.Fired != edgeFiredPending {
			continue
		}
		if anyTrue || anyPending || nonDefaultFired == 0 {
			continue
		}
		if err := s.db.MarkEdgeFired(ctx, defaultRow.ID, edgeFiredTrue, journal.ContentSHA); err != nil {
			s.log.Warn("edge.default_mark_failed",
				string(obs.KeyEdgeID), defaultRow.ID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		s.log.Info(string(obs.EventEdgeDefaultFallback),
			string(obs.KeyEdgeID), defaultRow.ID,
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

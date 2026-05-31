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
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

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
	edgeFiredTrue  = "true"
	edgeFiredFalse = "false"
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
}

// Scheduler is single-caller: Tick must not be invoked concurrently.
// The orchestrator's Run loop owns the only call site and is itself
// flock-guarded by PollOnce, so this contract holds in practice
// without an explicit mutex. Admin commands that want to peek at
// state must use the read-only state.DB queries directly rather than
// calling Tick.
type Scheduler struct {
	db   *state.DB
	cfg  Config
	logf func(format string, args ...any)
}

// New constructs a Scheduler. The Config is copied; later mutations to
// cfg.LaneCaps do not affect the running scheduler.
func New(db *state.DB, cfg Config) *Scheduler {
	caps := make(map[string]int, len(cfg.LaneCaps))
	for k, v := range cfg.LaneCaps {
		caps[k] = v
	}
	cfg.LaneCaps = caps
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = 15 * time.Minute
	}
	return &Scheduler{db: db, cfg: cfg, logf: func(string, ...any) {}}
}

// SetLogger installs a printf-style logger used for skip diagnostics
// (e.g. an agent skipped because its hotspot lock is held). Silent
// by default.
func (s *Scheduler) SetLogger(f func(format string, args ...any)) {
	if f != nil {
		s.logf = f
	}
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
// edge whose from_id is in status=merged. Per-edge errors slog.Warn
// and continue so one bad predicate cannot stall the rest of the
// tick (spec §3.9). The eval pass writes to work_item_edges only —
// ListSpawnable observes the updated fired column on the next
// materializePending call.
func (s *Scheduler) Tick(ctx context.Context) ([]int64, error) {
	if s.cfg.Evaluator != nil {
		if err := s.evalPendingEdges(ctx); err != nil {
			return nil, fmt.Errorf("scheduler: eval edges: %w", err)
		}
	}
	if _, err := s.db.ExpireStaleLocks(ctx, s.cfg.LockTTL); err != nil {
		return nil, fmt.Errorf("scheduler: expire stale locks: %w", err)
	}

	pending, err := s.materializePending(ctx)
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
				s.logf("scheduler: agent %d (%s) skipped: hotspot locked", a.ID, a.WorkItemID)
				continue
			}
			return reserved, fmt.Errorf("scheduler: acquire locks for agent %d: %w", a.ID, err)
		}
		if _, err := s.db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
			// Release the locks we just took so we don't deadlock the
			// next tick.
			if _, releaseErr := s.db.ReleaseAgentLocks(ctx, a.ID); releaseErr != nil {
				s.logf("scheduler: release locks for agent %d after transition failure: %v", a.ID, releaseErr)
			}
			return reserved, fmt.Errorf("scheduler: mark agent %d spawning: %w", a.ID, err)
		}
		occupancy[a.Lane]++
		reserved = append(reserved, a.ID)
	}
	return reserved, nil
}

// materializePending walks ListSpawnable (work_items LEFT JOIN
// agents WHERE agents.id IS NULL ...) and UpsertPending-s one
// agents row per missing work_item. Returns the union: every
// existing pending agent + every newly-materialized one, in
// db-natural order. Per spec §2.8 — Scheduler is the
// materialization point so the orphan-row class is impossible by
// construction.
//
// Per-item UpsertPending failures are logged and skipped rather than
// aborting the batch: a single bad row should not stall every other
// pending agent. Context cancellation still propagates.
func (s *Scheduler) materializePending(ctx context.Context) ([]state.Agent, error) {
	spawnable, err := s.db.ListSpawnable(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list spawnable: %w", err)
	}
	var failures int
	for _, w := range spawnable {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := s.db.UpsertPending(ctx, w.ID, w.Lane); err != nil {
			failures++
			s.logf("scheduler: materialize pending for %s failed: %v", w.ID, err)
			continue
		}
	}
	if failures > 0 {
		s.logf("scheduler: materialize pass completed with %d/%d failures", failures, len(spawnable))
	}
	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list pending: %w", err)
	}
	return pending, nil
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
// Per-edge eval errors slog.Warn and mark the edge fired='false'
// rather than halting the tick: a single bad predicate is a config
// bug operators see in logs, not a stop-the-world fault. Missing
// journal (ErrJournalNotFound) is logged at info and leaves the
// edge pending so the next tick retries once the spawner catches up.
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
				slog.Info("edge.eval_skipped_no_journal", "from_id", fromID)
				continue
			}
			slog.Warn("edge.journal_load_failed", "from_id", fromID, "err", err)
			continue
		}
		schema, _ := s.resolveSchema(fromID)
		nonDefaultAllFalse := true
		var defaultEdgeID int64
		for _, e := range group {
			if e.IsDefault {
				defaultEdgeID = e.ID
				continue
			}
			fired, reason, evalErr := s.cfg.Evaluator.Eval(ctx, e, schema, journal)
			if evalErr != nil {
				slog.Warn("edge.eval_error",
					"edge_id", e.ID, "program_id", e.ProgramID,
					"from", e.FromID, "to", e.ToID, "err", evalErr)
				fired = false
				reason = "eval-error"
			}
			firedStr := edgeFiredFalse
			if fired {
				firedStr = edgeFiredTrue
				nonDefaultAllFalse = false
			}
			if err := s.db.MarkEdgeFired(ctx, e.ID, firedStr, journal.ContentSHA); err != nil {
				slog.Warn("edge.mark_failed", "edge_id", e.ID, "err", err)
				continue
			}
			evt := "edge.fired"
			if !fired {
				evt = "edge.skipped"
			}
			slog.Info(evt, "edge_id", e.ID, "program_id", e.ProgramID,
				"from", e.FromID, "to", e.ToID,
				"predicate", e.PredicateCEL, "reason", reason,
				"journal_sha", journal.ContentSHA)
		}
		if defaultEdgeID != 0 && nonDefaultAllFalse {
			if err := s.db.MarkEdgeFired(ctx, defaultEdgeID, edgeFiredTrue, journal.ContentSHA); err != nil {
				slog.Warn("edge.default_mark_failed", "edge_id", defaultEdgeID, "err", err)
				continue
			}
			slog.Info("edge.default_fallback",
				"edge_id", defaultEdgeID, "from", fromID,
				"journal_sha", journal.ContentSHA)
		}
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

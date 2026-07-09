package scheduler

import (
	"context"
	"database/sql"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// reserveFromSpawnable walks the gate-pass-filtered spawnable slice and
// runs a single tx per work_item that upserts the pending agent +
// acquires hotspot locks + transitions to spawning. Lane-capped items
// still get their pending row materialized (committed in a no-transition
// tx) so the next tick's reserveOrphans pass picks them up once capacity
// frees.
func (s *Scheduler) reserveFromSpawnable(ctx context.Context, tc *tickCtx, spawnable []state.WorkItem, occupancy map[string]int) (reserved []int64, attempted map[int64]struct{}, err error) {
	attempted = map[int64]struct{}{}
	var failures int
	activeScopes := s.buildActiveFileScopes(ctx, tc)
	// reservedScopes captures scopes committed earlier in THIS tick so a
	// second same-tick candidate sees the first as in-flight; without it
	// 3 same-lane same-file candidates would all spawn on a cold start.
	reservedScopes := map[int64]activeScope{}
	collisions := 0
	candidates := 0
	for _, w := range spawnable {
		if err := ctx.Err(); err != nil {
			return reserved, attempted, err
		}
		candidates++
		if s.deferOnScopeCollision(ctx, w, activeScopes, reservedScopes, attempted) {
			collisions++
			continue
		}
		abort, failed := s.attemptReserveAndBook(ctx, tc, w, occupancy, attempted, reservedScopes, &reserved)
		if abort != nil {
			return reserved, attempted, abort
		}
		if failed {
			failures++
		}
	}
	if candidates > 0 && collisions == candidates {
		s.log.Warn(string(obs.EventSchedulerFileScopeCycleStalled),
			"candidates", candidates,
			"collisions", collisions,
		)
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

// deferOnScopeCollision returns true when w overlaps an active or
// same-tick-reserved file scope; the pending row is materialized so the
// next tick's reserveOrphans pass picks it up once the conflict clears.
func (s *Scheduler) deferOnScopeCollision(ctx context.Context, w state.WorkItem, activeScopes map[int64]activeScope, reservedScopes map[int64]activeScope, attempted map[int64]struct{}) bool {
	if s.cfg.FileScopeExtractor == nil {
		return false
	}
	conflict, overlap, ok := s.detectScopeCollision(w, activeScopes, reservedScopes)
	if !ok {
		return false
	}
	s.log.Info(string(obs.EventSchedulerFileScopeCollisionDeferred),
		string(obs.KeyWorkItemID), w.ID,
		string(obs.KeyLane), w.Lane,
		"conflicting_agent_id", conflict.agentID,
		"conflicting_work_item_id", conflict.workItemID,
		"overlap_paths", overlap,
	)
	if a, upErr := s.db.UpsertPending(ctx, w.ID, w.Lane); upErr == nil {
		attempted[a.ID] = struct{}{}
	} else {
		s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
			string(obs.KeyWorkItemID), w.ID,
			string(obs.KeyReason), "upsert_after_file_scope_defer_failed",
			string(obs.KeyErr), upErr.Error(),
		)
	}
	return true
}

// attemptReserveAndBook runs reserveOne under a work_item span, applies
// the lock-held retry + per-item swallow contract, and books occupancy
// / reservedScopes when the tx transitioned to spawning. Returns a
// non-nil abort err only for WriteHook crash-sim (spec §3.3) which must
// halt the tick.
func (s *Scheduler) attemptReserveAndBook(
	ctx context.Context,
	tc *tickCtx,
	w state.WorkItem,
	occupancy map[string]int,
	attempted map[int64]struct{},
	reservedScopes map[int64]activeScope,
	reserved *[]int64,
) (abort error, failed bool) {
	// W6 spec §3.5: one `work_item` span per work_item lifecycle
	// under the active `tick` span. Attrs match spec §4.1.
	itemCtx, itemSpan := s.tracer.Start(ctx, "work_item",
		trace.WithAttributes(
			attribute.String(string(obs.KeyWorkItemID), w.ID),
			attribute.String(string(obs.KeyLane), w.Lane),
			attribute.String("regatta.kind", string(w.Kind)),
		))
	hasCap := s.laneHasCapacity(w.Lane, tc.laneCaps, occupancy)
	agentID, transitioned, err := s.reserveOne(itemCtx, tc, w.ID, w.Lane, hasCap)
	itemSpan.End()
	if err != nil {
		var hookErr *writeHookErr
		if errors.As(err, &hookErr) {
			return err, false
		}
		if errors.Is(err, state.ErrLockHeld) {
			// The reservation tx rolled back; re-upsert without
			// locks so the pending row persists for the next tick
			// (matches pre-refactor materialize behavior). Mark
			// agent already-attempted so reserveOrphans does not
			// re-try the same lock acquisition this same tick.
			if hErr := s.fireWriteHook(tc); hErr != nil {
				return hErr, false
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
			return nil, false
		}
		// Per-item failure: log + skip rather than abort the
		// batch so one bad row cannot stall the queue.
		s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
			string(obs.KeyWorkItemID), w.ID,
			string(obs.KeyReason), "reserve_failed",
			string(obs.KeyErr), err.Error(),
		)
		return nil, true
	}
	if agentID != 0 {
		attempted[agentID] = struct{}{}
	}
	if transitioned {
		occupancy[w.Lane]++
		*reserved = append(*reserved, agentID)
		if s.cfg.FileScopeExtractor != nil {
			if paths := s.cfg.FileScopeExtractor(w); len(paths) > 0 {
				reservedScopes[agentID] = activeScope{workItemID: w.ID, paths: paths}
			}
		}
	}
	return nil, false
}

// reserveOne wraps the per-work-item reservation in a single tx. When
// hasCap is false the tx commits the upsert only — the row stays
// pending and the next tick's reserveOrphans retries locks+transition.
// transitioned=true only when the agent left pending for spawning
// inside this tx.
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

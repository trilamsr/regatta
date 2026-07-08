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
		if s.cfg.FileScopeExtractor != nil {
			if conflict, overlap, ok := s.detectScopeCollision(w, activeScopes, reservedScopes); ok {
				collisions++
				s.log.Info(string(obs.EventSchedulerFileScopeCollisionDeferred),
					string(obs.KeyWorkItemID), w.ID,
					string(obs.KeyLane), w.Lane,
					"conflicting_agent_id", conflict.agentID,
					"conflicting_work_item_id", conflict.workItemID,
					"overlap_paths", overlap,
				)
				// Materialize the pending row so the next tick's
				// reserveOrphans pass picks it up once the conflicting
				// agent terminates — mirrors the lane-capped path.
				if a, upErr := s.db.UpsertPending(ctx, w.ID, w.Lane); upErr == nil {
					attempted[a.ID] = struct{}{}
				} else {
					s.log.Warn(string(obs.EventSchedulerMaterializeFailure),
						string(obs.KeyWorkItemID), w.ID,
						string(obs.KeyReason), "upsert_after_file_scope_defer_failed",
						string(obs.KeyErr), upErr.Error(),
					)
				}
				continue
			}
		}
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
			// WriteHook crash-sim must abort the tick (spec §3.3) —
			// bypass the per-item swallow that protects against
			// production row-shape errors.
			var hookErr *writeHookErr
			if errors.As(err, &hookErr) {
				return reserved, attempted, err
			}
			if errors.Is(err, state.ErrLockHeld) {
				// The reservation tx rolled back; re-upsert without
				// locks so the pending row persists for the next tick
				// (matches pre-refactor materialize behavior). Mark
				// agent already-attempted so reserveOrphans does not
				// re-try the same lock acquisition this same tick.
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
			// Per-item failure: log + skip rather than abort the
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
			if s.cfg.FileScopeExtractor != nil {
				if paths := s.cfg.FileScopeExtractor(w); len(paths) > 0 {
					reservedScopes[agentID] = activeScope{workItemID: w.ID, paths: paths}
				}
			}
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

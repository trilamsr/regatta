package scheduler

import (
	"context"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// workItemBatchGetter is the schedulerDB upcast that lets snapshotWorkItems
// collapse the per-orphan + per-active-agent GetWorkItem loop into one
// batch fetch (#1359). Production *state.DB always satisfies both this
// and workItemGetter; test wrappers may strip one or the other.
type workItemBatchGetter interface {
	GetWorkItemsBatch(ctx context.Context, ids []string) (map[string]state.WorkItem, error)
}

// snapshotWorkItems primes tc.workItems with the union of spawnable +
// in-flight-agent + pending-orphan work_item IDs so downstream steps
// (buildActiveFileScopes, fetchWorkItemForRecheck, orphanArchivedNoGates)
// serve out of a per-tick map instead of one GetWorkItem per row.
// spawnable rows arrive fully materialized from ListSpawnable and land
// in the map directly; the remaining IDs are batch-fetched once. When
// the DB stripes off GetWorkItemsBatch the snapshot degrades to nil and
// callers fall back to per-id GetWorkItem — the pre-#1359 shape.
func (s *Scheduler) snapshotWorkItems(ctx context.Context, tc *tickCtx, spawnable []state.WorkItem) {
	batch, ok := s.db.(workItemBatchGetter)
	if !ok {
		return
	}
	seed := make(map[string]state.WorkItem, len(spawnable))
	for _, w := range spawnable {
		seed[w.ID] = w
	}
	need := map[string]struct{}{}
	active, err := s.db.ListAgentsByState(ctx, activeStates...)
	if err != nil {
		s.log.Warn("scheduler.workitem_snapshot_active_list_failed", string(obs.KeyErr), err.Error())
		return
	}
	for _, a := range active {
		if _, have := seed[a.WorkItemID]; have {
			continue
		}
		need[a.WorkItemID] = struct{}{}
	}
	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		s.log.Warn("scheduler.workitem_snapshot_pending_list_failed", string(obs.KeyErr), err.Error())
		return
	}
	for _, a := range pending {
		if _, have := seed[a.WorkItemID]; have {
			continue
		}
		need[a.WorkItemID] = struct{}{}
	}
	if len(need) > 0 {
		ids := make([]string, 0, len(need))
		for id := range need {
			ids = append(ids, id)
		}
		got, err := batch.GetWorkItemsBatch(ctx, ids)
		if err != nil {
			s.log.Warn("scheduler.workitem_snapshot_batch_failed",
				string(obs.KeyErr), err.Error(),
				"ids", len(ids),
			)
			return
		}
		for id, w := range got {
			seed[id] = w
		}
	}
	tc.workItems = seed
}

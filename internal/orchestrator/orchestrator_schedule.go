package orchestrator

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// ScheduleOnce ticks the scheduler and spawns every newly-reserved
// agent through the spawner. Each successful Spawn moves the agent
// spawning→running with the returned PID and session ID. A failed
// Spawn rolls the agent back to crashed → pending so a future tick
// can retry.
//
// A Tick that returns a partial reservation plus an error is handled
// by spawning the partial set first, then surfacing the Tick error.
// Returning early would otherwise strand the reserved agents in the
// spawning state with their locks held until the recovery sweep on
// the next restart.
func (o *Orchestrator) ScheduleOnce(ctx context.Context) error {
	// W6 spec §3.5: `tick` is the per-scheduler-tick span — the
	// orchestrator owns the open/close because it is the loop driver;
	// scheduler.Tick opens `work_item` children under the active ctx.
	ctx, span := o.tracer.Start(ctx, "tick")
	defer span.End()

	// Spec §3.3: tick.started + tick.completed are unconditional on
	// every tick exit; no early return may skip the completion event.
	startedAt := o.cfg.Clock()
	o.log.Info(string(obs.EventTickStarted))

	ids, tickErr := o.sched.Tick(ctx)
	evaluated := len(ids)
	defer func() {
		o.log.Info(string(obs.EventTickCompleted),
			string(obs.KeyDurationMs), o.cfg.Clock().Sub(startedAt).Milliseconds(),
			string(obs.KeyWorkItemsEvaluated), int64(evaluated),
		)
	}()

	for _, id := range ids {
		a, err := o.db.GetAgent(ctx, id)
		if err != nil {
			return fmt.Errorf("orchestrator: load agent %d: %w", id, err)
		}
		// #295: DAGID = parent program id when the work_item belongs to a
		// multi-feature program; otherwise the work_item is its own DAG
		// root and DAGID falls back to the work_item id. RunID = agent id
		// so retries of the same work_item produce distinct runs.
		// Lookup failure is non-fatal — the spawn proceeds with the
		// work_item-id fallback in spend.SpawnerCallback so a transient
		// state read does not strand the reservation.
		dagID := a.WorkItemID
		if wi, werr := o.db.GetWorkItem(ctx, a.WorkItemID); werr == nil && wi.ParentProgramID != "" {
			dagID = wi.ParentProgramID
		}
		var itemBody string
		if o.cfg.ItemBody != nil {
			if body, ok := o.cfg.ItemBody(ctx, a.WorkItemID); ok {
				itemBody = body
			} else {
				o.log.Warn("orchestrator.item_body_missing",
					string(obs.KeyWorkItemID), a.WorkItemID,
					string(obs.KeyAgentID), a.ID,
				)
			}
		}
		result, err := o.spawner.Spawn(ctx, spawner.Request{
			AgentID:    a.ID,
			WorkItemID: a.WorkItemID,
			Lane:       a.Lane,
			OperatorID: fmt.Sprintf("agent-%d", a.ID),
			DAGID:      dagID,
			RunID:      fmt.Sprintf("agent-%d", a.ID),
			ItemBody:   itemBody,
			RepoRoot:   o.cfg.RepoRoot,
		})
		if err != nil {
			_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{})
			_, _ = o.db.ReleaseAgentLocks(ctx, a.ID)
			_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{})
			_ = o.db.RecordEvent(ctx, a.ID, "spawn_failed", fmt.Sprintf(`{"error":%q}`, err.Error()))
			o.log.Warn(string(obs.EventSpawnFailed),
				string(obs.KeyAgentID), a.ID,
				string(obs.KeyWorkItemID), a.WorkItemID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		pid := result.PID
		sess := result.SessionID
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{
			PID:       &pid,
			SessionID: &sess,
		}); err != nil {
			return fmt.Errorf("orchestrator: mark agent %d running: %w", a.ID, err)
		}
		_ = o.db.RecordEvent(ctx, a.ID, "spawned",
			fmt.Sprintf(`{"pid":%d,"session_id":%q}`, pid, sess))
		o.log.Info(string(obs.EventSpawnCompleted),
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyLane), a.Lane,
			"pid", pid,
			"session_id", sess,
		)
	}
	if tickErr != nil {
		return fmt.Errorf("orchestrator: scheduler tick: %w", tickErr)
	}
	return nil
}

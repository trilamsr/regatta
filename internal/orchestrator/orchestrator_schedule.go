package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// slowTickThresholdMs is the duration above which ScheduleOnce emits obs.EventTickSlow at WARN. Default TickInterval is 5s (cmd/regatta/serve.go); 1s = 20% of a normal tick budget — generous enough to never fire on a hot path but tight enough to catch sqlite-lock contention or scheduler bugs.
const slowTickThresholdMs = 1000

// tickLogLevel picks DEBUG for zero-work ticks (operator log surface stays signal-rich) and INFO for non-zero ticks (dispatch activity loud). Closes #1066: 98% of dogfood-session log output was tick heartbeat.
func tickLogLevel(evaluated int) slog.Level {
	if evaluated == 0 {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

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

	// Spec §3.3: tick.started + tick.completed always fire — no early
	// return may skip the completion event. Severity is dynamic per
	// #1066: zero-work ticks log at DEBUG so the operator log surface
	// is not 98% scheduler heartbeat; non-zero ticks promote to INFO
	// so dispatch activity stays loud. The decision is captured after
	// scheduler.Tick returns so both started + completed events agree.
	startedAt := o.cfg.Clock()

	ids, tickErr := o.sched.Tick(ctx)
	evaluated := len(ids)
	level := tickLogLevel(evaluated)
	o.log.Log(ctx, level, string(obs.EventTickStarted))
	defer func() {
		durationMs := o.cfg.Clock().Sub(startedAt).Milliseconds()
		// R19-A O1: an NTP backwards step between startedAt and this
		// deferred read produces a negative duration that mis-fires the
		// slowTickThreshold comparison and ships nonsense in dashboards.
		if durationMs < 0 {
			durationMs = 0
		}
		o.log.Log(ctx, level, string(obs.EventTickCompleted),
			string(obs.KeyDurationMs), durationMs,
			string(obs.KeyWorkItemsEvaluated), int64(evaluated),
		)
		// Substrate liveness heartbeat: sibling-CLI `regatta status` reads
		// substrate-event mtime as the daemon-up signal. Without this, an
		// idle daemon (no spawn activity) emits zero substrate events and
		// status falsely reports "regatta serve not detected". Bounded
		// write rate at 5s tick: ~17k rows/day, comparable to spawn/merge
		// event volume on a busy session.
		_ = o.db.RecordEvent(ctx, 0, string(obs.EventTickCompleted),
			fmt.Sprintf(`{"duration_ms":%d,"work_items_evaluated":%d}`, durationMs, evaluated))
		if durationMs >= slowTickThresholdMs {
			o.log.Warn(string(obs.EventTickSlow),
				string(obs.KeyDurationMs), durationMs,
				string(obs.KeyWorkItemsEvaluated), int64(evaluated),
			)
		}
	}()

	for _, id := range ids {
		a, err := o.getAgent(ctx, id)
		if err != nil {
			// R19-A O2: one bad GetAgent must not abort the tick — siblings
			// in ids never get spawned, and the scheduler's reservation
			// state stays inconsistent until the recovery sweep. Log +
			// continue is the same shape as the spawn-failed branch below.
			o.log.Warn(string(obs.EventTickAgentLoadFailed),
				string(obs.KeyAgentID), id,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		// MAY-80: a work_item that recently failed to spawn is still in
		// its exponential cooldown — release the reservation back to
		// pending so a later tick can re-attempt, rather than re-spawning
		// blindly every 5s tick (the prior infinite-retry hammer).
		if !o.spawnBackoff.Admit(a.WorkItemID) {
			o.rollbackReservation(ctx, a)
			_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventSpawnBackoffSkipped), "{}")
			o.log.Info(string(obs.EventSpawnBackoffSkipped),
				string(obs.KeyAgentID), a.ID,
				string(obs.KeyWorkItemID), a.WorkItemID,
			)
			continue
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
			body, ok := o.cfg.ItemBody(ctx, a.WorkItemID)
			if !ok {
				// MAY-81: a configured loader that cannot resolve the brief
				// means the body is genuinely missing — spawning would burn
				// a real agent invocation on a context-less prompt. Hold the
				// item (release back to pending) so a later tick re-attempts
				// once the brief lands, instead of spawning blind.
				// MAY-90: register a backoff failure so the next tick's
				// Admit gate suppresses re-emission of spawn.held_no_body
				// instead of flooding the events log every tick.
				o.rollbackReservation(ctx, a)
				o.spawnBackoff.RecordFailure(a.WorkItemID)
				_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventSpawnHeldNoBody), "{}")
				o.log.Warn(string(obs.EventSpawnHeldNoBody),
					string(obs.KeyWorkItemID), a.WorkItemID,
					string(obs.KeyAgentID), a.ID,
				)
				continue
			}
			itemBody = body
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

			DestructiveOpsDeny:       o.cfg.DestructiveOpsDeny,
			AgentDestructiveOpsAllow: o.cfg.AgentDestructiveOpsAllow,
		})
		if err != nil {
			o.rollbackReservation(ctx, a)
			o.spawnBackoff.RecordFailure(a.WorkItemID)
			_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventSpawnFailed), fmt.Sprintf(`{"error":%q}`, err.Error()))
			o.log.Warn(string(obs.EventSpawnFailed),
				string(obs.KeyAgentID), a.ID,
				string(obs.KeyWorkItemID), a.WorkItemID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		o.spawnBackoff.RecordSuccess(a.WorkItemID)
		pid := result.PID
		sess := result.SessionID
		if _, err := o.transitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{
			PID:       &pid,
			SessionID: &sess,
		}); err != nil {
			// R19-A O3 (R19-A reviewer HIGH): spawn already returned a
			// live PID + session. Rolling the agent back to pending
			// (the prior shape) re-emits the same work_item on the next
			// tick because ListSpawnable filters `a.id IS NULL` — but a
			// pending row clears that gate, so the scheduler spawns a
			// SECOND process for the same work_item while the first PID
			// still runs. Stamp the PID + SessionID onto a crashed row
			// instead: ListSpawnable stays blocked (agent row exists,
			// state≠pending) AND the PID is durably attached for the
			// reaper to kill once crashed enters its terminal set
			// (follow-up tracked in PR body; reaper.terminal currently
			// {done, withdrawn, escalated}). Locks are released so the
			// lane is not wedged.
			_, terr := o.transitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{
				PID:       &pid,
				SessionID: &sess,
			})
			if terr != nil {
				o.log.Warn("orchestrator.post_transition_crashed_stamp_failed",
					string(obs.KeyAgentID), a.ID,
					string(obs.KeyErr), terr.Error(),
				)
			}
			_, _ = o.db.ReleaseAgentLocks(ctx, a.ID)
			_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventSpawnPostTransitionFailed), fmt.Sprintf(`{"error":%q,"pid":%d}`, err.Error(), pid))
			o.log.Warn(string(obs.EventSpawnPostTransitionFailed),
				string(obs.KeyAgentID), a.ID,
				string(obs.KeyWorkItemID), a.WorkItemID,
				string(obs.KeyErr), err.Error(),
				"pid", pid,
			)
			continue
		}
		_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventSpawnCompleted),
			fmt.Sprintf(`{"pid":%d,"session_id":%q}`, pid, sess))
		o.log.Info(string(obs.EventSpawnCompleted),
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyLane), a.Lane,
			"pid", pid,
			"session_id", sess,
		)
	}
	// Advance the spawn-backoff clock once per tick so suppression
	// windows opened by this tick's failures elapse on later ticks
	// (MAY-80).
	o.spawnBackoff.Tick()
	if tickErr != nil {
		return fmt.Errorf("orchestrator: scheduler tick: %w", tickErr)
	}
	return nil
}

// getAgent dispatches to the test override when set, else to the real
// state.DB. Keeps the production hot path as a single nil-check.
func (o *Orchestrator) getAgent(ctx context.Context, id int64) (*state.Agent, error) {
	if o.getAgentOverride != nil {
		return o.getAgentOverride(ctx, id)
	}
	return o.db.GetAgent(ctx, id)
}

// transitionAgent dispatches to the test override when set, else to
// the real state.DB. Symmetric with getAgent.
func (o *Orchestrator) transitionAgent(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
	if o.transitionAgentOverride != nil {
		return o.transitionAgentOverride(ctx, id, next, mut)
	}
	return o.db.TransitionAgent(ctx, id, next, mut)
}

// rollbackReservation releases a reserved agent back to pending with its
// lane lock freed, the shared crashed→pending→release path used when a
// spawn fails, is suppressed by backoff (MAY-80), or is held for a
// missing brief body (MAY-81). Best-effort: each transition error is
// swallowed because the recovery sweep reaps any agent left mid-rollback.
func (o *Orchestrator) rollbackReservation(ctx context.Context, a *state.Agent) {
	_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{})
	_, _ = o.db.ReleaseAgentLocks(ctx, a.ID)
	_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{})
}

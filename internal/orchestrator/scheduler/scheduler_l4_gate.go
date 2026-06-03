package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/trilamsr/regatta/internal/gates/l4"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler/filter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// eventKindGateRejected duplicates rejectionrouter.EventKindGateRejected
// to keep the scheduler → rejectionrouter dependency one-way.
const eventKindGateRejected = "gate_rejected"

// l4Scope pairs the resolver's Config + Input so filter.Pass[Scope]
// can carry both into the Evaluate closure.
type l4Scope struct {
	cfg l4.Config
	in  l4.Input
}

// applyL4Gate filters spawnable through the adversarial-reviewer gate
// (spec §3.2 step 0.7). Blocking=false keeps the wi; Blocking=true
// drops from this tick (status stays 'planned' so the next tick
// re-evaluates once the implementer pushes a new SHA). Disabled
// (L4Gate or L4GateResolver nil) returns spawnable byte-equal — zero
// overhead. Per-wi Evaluate errors warn and treat as denied
// (fail-closed). Severity-block routing lives inside l4.Run; the
// scheduler consumes GateResult.Blocking as the single drop signal.
func (s *Scheduler) applyL4Gate(ctx context.Context, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.L4Gate == nil || s.cfg.L4GateResolver == nil {
		return spawnable, nil
	}
	return filter.Apply(ctx, filter.Pass[l4Scope]{
		Name: "l4",
		Resolve: func(wi state.WorkItem) (l4Scope, bool) {
			cfg, in, gated := s.cfg.L4GateResolver(wi)
			return l4Scope{cfg: cfg, in: in}, gated
		},
		Evaluate: func(ctx context.Context, wi state.WorkItem, sc l4Scope) (bool, error) {
			gr, err := s.cfg.L4Gate.Evaluate(ctx, sc.cfg, sc.in)
			if err != nil {
				s.log.Warn("scheduler.l4_gate_error",
					string(obs.KeyWorkItemID), wi.ID,
					string(obs.KeyGateID), sc.cfg.GateID,
					string(obs.KeyErr), err.Error(),
				)
				return false, nil
			}
			if !gr.Blocking {
				return true, nil
			}
			reason := string(gr.Verdict)
			if len(gr.Findings) > 0 {
				reason = gr.Findings[0].ID
			}
			s.log.Info("scheduler.l4_gate_blocked",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyGateID), sc.cfg.GateID,
				string(obs.KeyVerdict), string(gr.Verdict),
				string(obs.KeyReason), reason,
			)
			s.emitGateRejected(ctx, wi.ID, sc.in.PRSHA, reason)
			return false, nil
		},
	}, spawnable)
}

// emitGateRejected writes the audit row the RejectionRouter drains
// (issue #479). agent_id resolves to the agents row when one exists;
// sql.ErrNoRows → agent_id=NULL because the gate can fire before the
// wi is ever reserved. Emit errors warn rather than abort the tick — a
// missed audit row is recoverable, but a failed tick drops every wi
// behind this one.
func (s *Scheduler) emitGateRejected(ctx context.Context, workItemID, prSHA, reason string) {
	var agentID int64
	a, err := s.db.GetAgentByWorkItemID(ctx, workItemID)
	switch {
	case err == nil:
		agentID = a.ID
	case errors.Is(err, sql.ErrNoRows):
		// audit-only: no agent yet
	default:
		s.log.Warn("scheduler.l4_gate_emit_lookup_error",
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	payload, err := json.Marshal(struct {
		PRSHA  string `json:"pr_sha"`
		Reason string `json:"reason"`
	}{PRSHA: prSHA, Reason: reason})
	if err != nil {
		s.log.Warn("scheduler.l4_gate_emit_marshal_error",
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	if err := s.db.RecordEvent(ctx, agentID, eventKindGateRejected, string(payload)); err != nil {
		s.log.Warn("scheduler.l4_gate_emit_record_error",
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyErr), err.Error(),
		)
	}
}

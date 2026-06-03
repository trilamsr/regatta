package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// eventKindGateRejected duplicates rejectionrouter.EventKindGateRejected
// to keep the scheduler → rejectionrouter dependency one-way (the
// router is the downstream consumer of scheduler-emitted events). Both
// packages assert the constant against the same string literal.
const eventKindGateRejected = "gate_rejected"

// applyL4Gate filters spawnable through the adversarial-reviewer gate
// (spec §3.2 step 0.7). Blocking=false keeps the wi; Blocking=true
// drops from this tick (status stays 'planned' so the next tick
// re-evaluates once the implementer pushes a new SHA).
//
// Disabled (L4Gate or L4GateResolver nil) returns spawnable byte-equal
// — zero overhead, matches the cost-governor short-circuit.
// Per-wi Evaluate errors warn and treat as denied (fail-closed,
// matches the cost gate).
//
// Severity-block routing lives inside l4.Run; the scheduler consumes
// GateResult.Blocking as the single drop signal so the yaml
// severity_block DSL stays a property of the gate, not the scheduler.
func (s *Scheduler) applyL4Gate(ctx context.Context, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.L4Gate == nil || s.cfg.L4GateResolver == nil {
		return spawnable, nil
	}
	kept := make([]state.WorkItem, 0, len(spawnable))
	for _, wi := range spawnable {
		cfg, in, gated := s.cfg.L4GateResolver(wi)
		if !gated {
			kept = append(kept, wi)
			continue
		}
		gr, err := s.cfg.L4Gate.Evaluate(ctx, cfg, in)
		if err != nil {
			s.log.Warn("scheduler.l4_gate_error",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyGateID), cfg.GateID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		if gr.Blocking {
			reason := string(gr.Verdict)
			if len(gr.Findings) > 0 {
				reason = gr.Findings[0].ID
			}
			s.log.Info("scheduler.l4_gate_blocked",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyGateID), cfg.GateID,
				string(obs.KeyVerdict), string(gr.Verdict),
				string(obs.KeyReason), reason,
			)
			s.emitGateRejected(ctx, wi.ID, in.PRSHA, reason)
			continue
		}
		kept = append(kept, wi)
	}
	return kept, nil
}

// emitGateRejected writes the audit row the RejectionRouter drains
// (issue #479). agent_id resolves to the agents row when one exists;
// sql.ErrNoRows → agent_id=NULL because the gate can fire before the
// wi is ever reserved. Emit errors warn rather than abort the tick —
// a missed audit row is recoverable on re-evaluation, but a failed
// tick drops every wi behind this one.
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
		// Hand-built struct; marshal cannot fail in practice.
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

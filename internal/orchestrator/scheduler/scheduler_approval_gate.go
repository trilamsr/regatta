package scheduler

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// applyApprovalGates filters spawnable through the HITL gate (spec
// §3.1 step 0.5). Proceed keeps the wi; Pause drops from this tick;
// Reject drops AND flips status to 'rejected' so ListSpawnable no
// longer surfaces it. Disabled (Gate or GateResolver nil) returns
// spawnable unchanged.
//
// Per-wi evaluation errors warn and pause for this tick — fail-closed
// posture (spec §3.2): a misconfigured gate must not advance a wi.
// Reject-transition failures DO halt the tick because a wi stuck in
// 'planned' after a reject verdict would silently retry.
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
		// work_item span.
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
			// ResultPause (zero value) — approval row persists; next
			// Tick re-Evaluates until a reviewer decides or the reaper
			// times it out (spec §3.3).
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
// via TransitionWorkItem. The CAS predicate is load-bearing: a
// concurrent writer that already moved the row (cancel/archive) must
// win — the gate cannot resurrect a terminal wi.
// ErrInvalidWorkItemTransition surfaces unwrapped so the caller can
// errors.Is on the lost race.
func (s *Scheduler) markWorkItemRejected(ctx context.Context, tc *tickCtx, id string) error {
	if err := s.fireWriteHook(tc); err != nil {
		return err
	}
	return s.db.TransitionWorkItem(ctx, id, state.WorkStatusPlanned, state.WorkStatusRejected)
}

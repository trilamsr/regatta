package scheduler

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler/filter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// applyApprovalGates filters spawnable through the HITL gate (spec
// §3.1 step 0.5). Proceed keeps the wi; Pause drops from this tick;
// Reject drops AND flips status to 'rejected' so ListSpawnable no
// longer surfaces it. Disabled (Gate or GateResolver nil) returns
// spawnable unchanged. Per-wi evaluation errors warn and pause
// (fail-closed §3.2); reject-transition failure halts the tick because
// a wi stuck in 'planned' after a reject verdict would silently retry.
func (s *Scheduler) applyApprovalGates(ctx context.Context, tc *tickCtx, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.Gate == nil || s.cfg.GateResolver == nil {
		return spawnable, nil
	}
	return filter.Apply(ctx, filter.Pass[approval.Config]{
		Name:    "approval",
		Resolve: s.cfg.GateResolver,
		Evaluate: func(ctx context.Context, wi state.WorkItem, cfg approval.Config) (bool, error) {
			// W6 §3.5: gate.evaluate must be a child of the work_item span.
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
				return false, nil
			}
			switch res {
			case approval.ResultProceed:
				return true, nil
			case approval.ResultReject:
				if err := s.markWorkItemRejected(ctx, tc, wi.ID); err != nil {
					return false, fmt.Errorf("mark %s rejected: %w", wi.ID, err)
				}
				s.log.Info(string(obs.EventApprovalDecided),
					string(obs.KeyWorkItemID), wi.ID,
					string(obs.KeyGateID), cfg.Name,
					string(obs.KeyVerdict), approval.ResultReject.String(),
				)
				return false, nil
			default:
				s.log.Info(string(obs.EventApprovalDecided),
					string(obs.KeyWorkItemID), wi.ID,
					string(obs.KeyGateID), cfg.Name,
					string(obs.KeyVerdict), approval.ResultPause.String(),
				)
				return false, nil
			}
		},
	}, spawnable)
}

// markWorkItemRejected flips work_items.status='rejected' atomically
// via TransitionWorkItem. The CAS predicate is load-bearing: a
// concurrent writer that already moved the row (cancel/archive) must
// win — the gate cannot resurrect a terminal wi.
func (s *Scheduler) markWorkItemRejected(ctx context.Context, tc *tickCtx, id string) error {
	if err := s.fireWriteHook(tc); err != nil {
		return err
	}
	return s.db.TransitionWorkItem(ctx, id, state.WorkStatusPlanned, state.WorkStatusRejected)
}

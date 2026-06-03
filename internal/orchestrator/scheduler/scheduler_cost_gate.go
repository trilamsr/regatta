package scheduler

import (
	"context"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler/filter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// applyCostGovernor filters spawnable through the cost-governor
// pre-call deny gate (spec §3.2 step 0.6). Allow=true keeps the wi;
// Allow=false drops from this tick (status stays 'planned' so the next
// tick re-evaluates after the reconciler catches up). Disabled (CostGate
// or CostGateResolver nil) returns spawnable byte-equal — zero overhead
// (I6 §8). Per-wi errors warn and treat as denied (fail-closed, matches
// the approval gate). Soft-cap downgrade (R10): SoftCapBreached AND
// scope.AllowDowngrade fires OnDowngrade with the suggested model so
// spawner's Request.ModelOverride can carry the swap.
func (s *Scheduler) applyCostGovernor(ctx context.Context, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.CostGate == nil || s.cfg.CostGateResolver == nil {
		return spawnable, nil
	}
	return filter.Apply(ctx, filter.Pass[gate.WorkItemScope]{
		Name:    "cost",
		Resolve: s.cfg.CostGateResolver,
		Evaluate: func(ctx context.Context, wi state.WorkItem, scope gate.WorkItemScope) (bool, error) {
			v, err := s.cfg.CostGate.Evaluate(ctx, scope)
			if err != nil {
				s.log.Warn("scheduler.cost_gate_error",
					string(obs.KeyWorkItemID), wi.ID,
					string(obs.KeyErr), err.Error(),
				)
				return false, nil
			}
			if !v.Allow {
				s.log.Info("scheduler.cost_gate_denied",
					string(obs.KeyWorkItemID), wi.ID,
					string(obs.KeyReason), v.Reason,
				)
				return false, nil
			}
			if v.SoftCapBreached && v.DowngradeTo != "" && s.cfg.OnDowngrade != nil {
				s.cfg.OnDowngrade(wi.ID, v.DowngradeTo)
			}
			return true, nil
		},
	}, spawnable)
}

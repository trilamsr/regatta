package scheduler

import (
	"context"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// applyCostGovernor filters spawnable through the cost-governor
// pre-call deny gate (spec §3.2 step 0.6). Allow=true keeps the wi;
// Allow=false drops from this tick (status stays 'planned' so the next
// tick re-evaluates after the reconciler catches up).
//
// Disabled (CostGate or CostGateResolver nil) returns spawnable
// byte-equal — ZERO overhead (closes I6 per spec §8 row 1).
//
// Per-wi errors warn and treat as denied (fail-closed, matches the
// approval gate). Soft-cap downgrade (spec R10): when
// Verdict.SoftCapBreached AND scope.AllowDowngrade, OnDowngrade fires
// with the suggested model so spawner's Request.ModelOverride can
// carry the swap (Wave 2 consumer).
func (s *Scheduler) applyCostGovernor(ctx context.Context, spawnable []state.WorkItem) ([]state.WorkItem, error) {
	if s.cfg.CostGate == nil || s.cfg.CostGateResolver == nil {
		return spawnable, nil
	}
	kept := make([]state.WorkItem, 0, len(spawnable))
	for _, wi := range spawnable {
		scope, ok := s.cfg.CostGateResolver(wi)
		if !ok {
			kept = append(kept, wi)
			continue
		}
		v, err := s.cfg.CostGate.Evaluate(ctx, scope)
		if err != nil {
			s.log.Warn("scheduler.cost_gate_error",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		if !v.Allow {
			s.log.Info("scheduler.cost_gate_denied",
				string(obs.KeyWorkItemID), wi.ID,
				string(obs.KeyReason), v.Reason,
			)
			continue
		}
		if v.SoftCapBreached && v.DowngradeTo != "" && s.cfg.OnDowngrade != nil {
			s.cfg.OnDowngrade(wi.ID, v.DowngradeTo)
		}
		kept = append(kept, wi)
	}
	return kept, nil
}

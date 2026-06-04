package scheduler

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// CostCapGate is the scheduler-side seam to the global daily-spend
// ceiling (cost.cap, spec PHASE-AUTONOMY W5). Allow=false halts the
// ENTIRE tick — no per-work-item evaluation, no spawn. nil = identity
// (zero overhead when cap is unset).
type CostCapGate interface {
	Allow(ctx context.Context) bool
}

// applyCostCap runs the W5 global-cap pre-filter BEFORE the per-scope
// gate so a saturated 24h budget pauses every spawn — not just
// individual per-DAG / per-operator breaches. Returns spawnable
// byte-equal when cap unset/below threshold; nil when throttled.
// INFO-logs once per tick so the operator sees the reason without
// grepping substrate (the cap enforcer emits the audit event on
// Active→Throttled).
func (s *Scheduler) applyCostCap(ctx context.Context, spawnable []state.WorkItem) []state.WorkItem {
	if s.cfg.CostCap == nil {
		return spawnable
	}
	if s.cfg.CostCap.Allow(ctx) {
		return spawnable
	}
	s.log.Info("scheduler.cost_cap_throttled", "throttled_work_items", len(spawnable))
	return nil
}

package scheduler

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// CostCapGate is the scheduler-side seam to the global daily-spend
// ceiling (cost.cap, spec PHASE-AUTONOMY W5); Allow=false halts the
// ENTIRE tick, nil = identity (zero overhead when cap unset).
type CostCapGate func(ctx context.Context) bool

// applyCostCap runs the W5 global-cap pre-filter BEFORE the per-scope
// gate so a saturated 24h budget pauses every spawn. Returns spawnable
// byte-equal when cap unset/below threshold; nil when throttled.
// INFO-logs once per tick so the operator sees the reason.
func (s *Scheduler) applyCostCap(ctx context.Context, spawnable []state.WorkItem) []state.WorkItem {
	if s.cfg.CostCap == nil {
		return spawnable
	}
	if s.cfg.CostCap(ctx) {
		return spawnable
	}
	s.log.Info("scheduler.cost_cap_throttled", "throttled_work_items", len(spawnable))
	return nil
}

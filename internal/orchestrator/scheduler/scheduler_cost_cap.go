package scheduler

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// CostCapGate is the scheduler-side seam to the global daily-spend
// ceiling (cost.cap, spec PHASE-AUTONOMY W5). Allow=false halts the
// ENTIRE tick — no per-work-item evaluation, no spawn. nil short-
// circuits to identity (zero overhead when cap is unset).
type CostCapGate interface {
	Allow(ctx context.Context) bool
}

// applyCostCap is the W5 global-cap pre-filter. Runs BEFORE the
// per-scope cost gate so a saturated 24h budget pauses every spawn,
// not just per-DAG / per-operator individual breaches. Returns the
// input slice byte-equal when the cap is unset or below threshold;
// returns empty slice when throttled (the per-work-item gate then
// skips entirely).
//
// Disabled (CostCap nil) returns spawnable byte-equal — ZERO overhead.
func (s *Scheduler) applyCostCap(ctx context.Context, spawnable []state.WorkItem) []state.WorkItem {
	if s.cfg.CostCap == nil {
		return spawnable
	}
	if s.cfg.CostCap.Allow(ctx) {
		return spawnable
	}
	// Throttled. Log once per tick at INFO so the operator sees the
	// reason without grepping substrate. The cap enforcer itself emits
	// the audit event (once per Active→Throttled transition).
	s.log.Info("scheduler.cost_cap_throttled", "throttled_work_items", len(spawnable))
	return nil
}

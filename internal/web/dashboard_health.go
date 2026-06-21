package web

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// dashboardHealthSnapshot is the single read-model the health surface, the
// pipeline-counts surface, and the work-items Running bucket all consume.
// One DB round trip per panel poll closes the cross-panel-drift class
// (#1217) where N separate count queries against agents / work_items
// returned divergent Running counts.
type dashboardHealthSnapshot struct {
	AgentStateCounts       map[state.AgentState]int
	WorkItemStatusCounts   map[state.WorkItemStatus]int
	TickIntervalSec        int64
	TickLastSuccessAgeSec  int64
	TickStaleBannerVisible bool
	LastExitReasonHistogram []exitReasonHistogramRow
}

// exitReasonHistogramRow renders one bar in the exit-reason histogram.
type exitReasonHistogramRow struct {
	Reason string
	Count  int
}

// loadHealthSnapshotView returns one consolidated read used by every
// health-related panel. Stub pending impl — RED commit.
func loadHealthSnapshotView(ctx context.Context, deps Dependencies) any {
	_ = ctx
	_ = deps
	return dashboardHealthSnapshot{
		AgentStateCounts:     map[state.AgentState]int{},
		WorkItemStatusCounts: map[state.WorkItemStatus]int{},
	}
}

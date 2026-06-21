package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// dashboardHealthSnapshot is the single read-model the health surface, the
// pipeline-counts surface, and the work-items Running bucket all consume.
// One GROUP BY agents.state + one GROUP BY work_items.status per panel
// poll closes the cross-panel-drift class (#1217) where N separate count
// queries returned divergent Running counts.
type dashboardHealthSnapshot struct {
	AgentStateCounts        map[state.AgentState]int
	WorkItemStatusCounts    map[state.WorkItemStatus]int
	TickIntervalSec         int64
	TickLastSuccessAgeSec   int64
	TickStaleBannerVisible  bool
	LastExitReasonHistogram []exitReasonHistogramRow
}

// exitReasonHistogramRow is one bar in the 1h exit-reason histogram.
type exitReasonHistogramRow struct {
	Reason string
	Count  int
}

// loadHealthSnapshotView returns one consolidated read used by every
// health-related panel. See dashboardHealthSnapshot for the drift story.
func loadHealthSnapshotView(ctx context.Context, deps Dependencies) any {
	snap := dashboardHealthSnapshot{
		AgentStateCounts:     map[state.AgentState]int{},
		WorkItemStatusCounts: map[state.WorkItemStatus]int{},
		TickIntervalSec:      tickIntervalSecondsOrDefault(deps.TickInterval),
	}
	if deps.DB == nil {
		return snap
	}
	sqlDB := deps.DB.SQL()
	if rows, err := sqlDB.QueryContext(ctx, `SELECT state, COUNT(*) FROM agents GROUP BY state`); err == nil {
		for rows.Next() {
			var s string
			var n int
			if err := rows.Scan(&s, &n); err == nil {
				snap.AgentStateCounts[state.AgentState(s)] = n
			}
		}
		_ = rows.Close()
	}
	if rows, err := sqlDB.QueryContext(ctx, `SELECT status, COUNT(*) FROM work_items GROUP BY status`); err == nil {
		for rows.Next() {
			var s string
			var n int
			if err := rows.Scan(&s, &n); err == nil {
				snap.WorkItemStatusCounts[state.WorkItemStatus(s)] = n
			}
		}
		_ = rows.Close()
	}
	now := deps.Clock()
	since := now.Add(-exitReasonHistogramWindowSec * time.Second).Unix()
	snap.LastExitReasonHistogram = exitReasonHistogram1h(ctx, deps, since)

	if deps.Heartbeat != nil {
		snap.TickLastSuccessAgeSec = int64(deps.Heartbeat.Age().Seconds())
		if snap.TickLastSuccessAgeSec > tickStaleBannerMultiplier*snap.TickIntervalSec {
			snap.TickStaleBannerVisible = true
		}
	}
	return snap
}

// tickIntervalSecondsOrDefault returns the configured tick interval as
// whole seconds, falling back to defaultTickIntervalSec on zero so a
// wiring miss does not paint the banner red on every poll.
func tickIntervalSecondsOrDefault(d time.Duration) int64 {
	if s := int64(d / time.Second); s > 0 {
		return s
	}
	return defaultTickIntervalSec
}

// exitReasonHistogram1h scans agent.exited events since `since` and tallies
// distinct exit_reason values. Result is descending-count then
// alphabetical so test assertions stay stable.
func exitReasonHistogram1h(ctx context.Context, deps Dependencies, since int64) []exitReasonHistogramRow {
	tally := map[string]int{}
	rows, err := deps.DB.SQL().QueryContext(ctx,
		`SELECT payload_json FROM events WHERE kind = 'agent.exited' AND created_at >= ?`, since)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		reason := exitReasonOrUnknown(payload)
		tally[reason]++
	}
	out := make([]exitReasonHistogramRow, 0, len(tally))
	for r, n := range tally {
		out = append(out, exitReasonHistogramRow{Reason: r, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// exitReasonOrUnknown unwraps the exit_reason field; empty / malformed
// payload falls back to exitReasonUnknown so drift surfaces visibly
// instead of getting silently dropped from the histogram.
func exitReasonOrUnknown(payload string) string {
	if payload == "" {
		return exitReasonUnknown
	}
	var p struct {
		ExitReason string `json:"exit_reason"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil || p.ExitReason == "" {
		return exitReasonUnknown
	}
	return p.ExitReason
}

// serveHealthPanel renders the consolidated health surface — tick-stale
// banner + agent-state cells + exit-reason histogram in one panel.
func serveHealthPanel(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	serveDashboardPanel(w, r, deps, "_health", loadHealthSnapshotView)
}

package web

import (
	"context"
	"encoding/json"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// loadDockerSoakView reports daemon uptime + last-1m spawn/exit tallies + latest exit_reason + a health pill so the operator does not have to tail docker logs to know whether the live daemon is healthy.
func loadDockerSoakView(ctx context.Context, deps Dependencies) any {
	view := dashboardDockerSoakView{Health: healthGreen, HealthLabel: "IDLE"}
	now := deps.Clock()
	booted := deps.BootedAt
	if booted.IsZero() {
		booted = now
	}
	view.Uptime = humanizeDuration(now.Sub(booted))
	if deps.DB == nil {
		return view
	}
	since := now.Add(-dockerSoakWindowSeconds * time.Second).Unix()
	spawns, exited, lastReason, lastPayload, nonCompleted, completed := tallyDockerSoakWindow(ctx, deps.DB, since)
	view.SpawnsLast1m = spawns
	view.ExitedLast1m = exited
	view.LastExitReason = lastReason
	view.HasLastExit = lastReason != ""
	view.LastExitBadge = exitReasonBadge(lastPayload)
	switch {
	case exited > 0 && nonCompleted == 0:
		view.Health, view.HealthLabel = healthGreen, "HEALTHY"
	case exited > 0 && completed == 0:
		view.Health, view.HealthLabel = healthRed, "DEGRADED"
	case nonCompleted > 0:
		view.Health, view.HealthLabel = healthAmber, "DEGRADING"
	}
	return view
}

// tallyDockerSoakWindow scans events since `since` and returns spawn/exit counts, latest exit_reason, its payload, and split completed/non-completed exit tallies.
func tallyDockerSoakWindow(ctx context.Context, db *state.DB, since int64) (spawns, exited int, lastReason, lastPayload string, nonCompleted, completed int) {
	rows, err := db.SQL().QueryContext(ctx,
		`SELECT kind, payload_json FROM events WHERE created_at >= ? AND kind IN ('spawn.started','agent.exited') ORDER BY id ASC`,
		since)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, payload string
		if err := rows.Scan(&kind, &payload); err != nil {
			continue
		}
		if kind == "spawn.started" {
			spawns++
			continue
		}
		exited++
		var p struct {
			ExitReason string `json:"exit_reason"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			continue
		}
		if p.ExitReason != "" {
			lastReason = p.ExitReason
			lastPayload = payload
		}
		switch p.ExitReason {
		case exitReasonCompleted:
			completed++
		default:
			// empty exit_reason = classifier didn't tag (data drift, pre-#1063 row, or payload schema break). Treat as non-completed so the HEALTHY-pill case (exited>0 && nonCompleted==0) cannot mask silent data corruption.
			nonCompleted++
		}
	}
	return
}

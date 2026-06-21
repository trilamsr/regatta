package web

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestLoadDockerSoakView_Healthy asserts green health pill when last 1m has spawns and no non-completed exits.
func TestLoadDockerSoakView_Healthy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-healthy.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-A", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"completed","exit_code":0}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-90 * time.Second)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "green" {
		t.Fatalf("Health=%q, want green", sv.Health)
	}
	if sv.SpawnsLast1m != 1 {
		t.Fatalf("SpawnsLast1m=%d, want 1", sv.SpawnsLast1m)
	}
	if sv.ExitedLast1m != 1 {
		t.Fatalf("ExitedLast1m=%d, want 1", sv.ExitedLast1m)
	}
	if sv.Uptime == "" {
		t.Fatalf("Uptime empty")
	}
	if sv.LastExitReason != "completed" {
		t.Fatalf("LastExitReason=%q, want completed", sv.LastExitReason)
	}
}

// TestLoadDockerSoakView_AmberOnMixedExits asserts amber health pill when any non-completed exit lands in last 1m alongside a completed one.
func TestLoadDockerSoakView_AmberOnMixedExits(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-amber.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-B", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"completed"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"tool_denied"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-5 * time.Minute)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "amber" {
		t.Fatalf("Health=%q, want amber", sv.Health)
	}
	if sv.ExitedLast1m != 2 {
		t.Fatalf("ExitedLast1m=%d, want 2", sv.ExitedLast1m)
	}
}

// TestLoadDockerSoakView_RedOnAllNonCompleted asserts red health pill when all last-1m exits are non-completed.
func TestLoadDockerSoakView_RedOnAllNonCompleted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-red.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-C", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_credit_exhausted"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_rate_limited"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-time.Hour)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "red" {
		t.Fatalf("Health=%q, want red", sv.Health)
	}
	if sv.LastExitReason != "provider_rate_limited" {
		t.Fatalf("LastExitReason=%q, want provider_rate_limited", sv.LastExitReason)
	}
}

// TestLoadDockerSoakView_Idle asserts the IDLE state on a fresh boot with no spawns or exits.
func TestLoadDockerSoakView_Idle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-idle.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-30 * time.Second)}
	view := loadDockerSoakView(context.Background(), deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "green" {
		t.Fatalf("Health=%q, want green on idle", sv.Health)
	}
	if sv.HealthLabel != "IDLE" {
		t.Fatalf("HealthLabel=%q, want IDLE", sv.HealthLabel)
	}
	if sv.SpawnsLast1m != 0 || sv.ExitedLast1m != 0 {
		t.Fatalf("idle counts spawns=%d exits=%d, want 0/0", sv.SpawnsLast1m, sv.ExitedLast1m)
	}
}

// TestLoadDockerSoakView_EmptyExitReasonNotMaskedAsHealthy asserts an agent.exited row whose exit_reason is empty (classifier didn't tag; pre-#1063 row) counts as non-completed so the HEALTHY pill cannot mask data drift.
func TestLoadDockerSoakView_EmptyExitReasonNotMaskedAsHealthy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-empty-reason.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-EMPTY", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent spawn: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_code":1}`); err != nil {
		t.Fatalf("RecordEvent exit: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-60 * time.Second)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T", view)
	}
	if sv.Health == "green" {
		t.Fatalf("empty-exit_reason masked as healthy: Health=%q HealthLabel=%q", sv.Health, sv.HealthLabel)
	}
}

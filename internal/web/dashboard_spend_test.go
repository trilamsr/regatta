package web

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestLoadSpendView_AllZeroSetsEmptyReason: when no token_spend rows exist and an agent.exited event carries exit_reason=provider_credit_exhausted in the last 24h, loadSpendView surfaces an EmptyReason + CreditExhaustedCount so the operator reads "no agents completed" instead of "spend tracking broken".
func TestLoadSpendView_AllZeroSetsEmptyReason(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	dsn := state.DSN(dbPath)
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), dsn, clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_credit_exhausted","exit_code":1}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	view := loadSpendView(ctx, Dependencies{DB: db, Clock: clock})
	sv, ok := view.(dashboardSpendView)
	if !ok {
		t.Fatalf("loadSpendView returned %T, want dashboardSpendView", view)
	}
	if sv.Last24hMicros != 0 || sv.TodayMicros != 0 || sv.LifetimeMicros != 0 {
		t.Fatalf("expected zero spend, got 24h=%d today=%d lifetime=%d", sv.Last24hMicros, sv.TodayMicros, sv.LifetimeMicros)
	}
	if sv.EmptyReason == "" {
		t.Fatalf("EmptyReason empty; want non-empty when all-zero + exited-with-reason events present")
	}
	if sv.CreditExhaustedCount != 1 {
		t.Fatalf("CreditExhaustedCount=%d, want 1", sv.CreditExhaustedCount)
	}
}

// TestLoadSpendView_ZeroSpendNoExitEvents asserts the empty-state annotation does NOT fire when the daemon has zero events at all — avoids false-positive "agents exited before reporting" copy on a fresh boot.
func TestLoadSpendView_ZeroSpendNoExitEvents(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "spend-empty.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	view := loadSpendView(context.Background(), Dependencies{DB: db, Clock: clock})
	sv, ok := view.(dashboardSpendView)
	if !ok {
		t.Fatalf("loadSpendView returned %T, want dashboardSpendView", view)
	}
	if sv.EmptyReason != "" {
		t.Fatalf("EmptyReason set on clean zero-event boot: %q", sv.EmptyReason)
	}
	if sv.CreditExhaustedCount != 0 {
		t.Fatalf("CreditExhaustedCount=%d on zero-event boot, want 0", sv.CreditExhaustedCount)
	}
}

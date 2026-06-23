package state_test

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestScheduler_BackoffPersistsAcrossRestart asserts UpsertBackoff round-trips through ListBackoff (R-MEGA-2 P4).
func TestScheduler_BackoffPersistsAcrossRestart(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	retry := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	in := state.BackoffEntry{WorkItemID: "WI-1", AttemptCount: 3, RetryAt: retry}
	if err := db.UpsertBackoff(ctx, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	rows, err := db.ListBackoff(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	got := rows[0]
	if got.WorkItemID != "WI-1" || got.AttemptCount != 3 || !got.RetryAt.Equal(retry) {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, in)
	}
}

// TestScheduler_BackoffUpsertReplaces asserts a second UpsertBackoff replaces (R-MEGA-2 P4).
func TestScheduler_BackoffUpsertReplaces(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	first := state.BackoffEntry{WorkItemID: "WI-1", AttemptCount: 1, RetryAt: time.Unix(1, 0).UTC()}
	if err := db.UpsertBackoff(ctx, first); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	second := state.BackoffEntry{WorkItemID: "WI-1", AttemptCount: 5, RetryAt: time.Unix(99, 0).UTC()}
	if err := db.UpsertBackoff(ctx, second); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	rows, err := db.ListBackoff(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	if rows[0].AttemptCount != 5 {
		t.Fatalf("AttemptCount=%d want 5", rows[0].AttemptCount)
	}
}

// TestScheduler_BackoffDelete drops the row on success.
func TestScheduler_BackoffDelete(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	if err := db.UpsertBackoff(ctx, state.BackoffEntry{WorkItemID: "WI-1", RetryAt: time.Now()}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.DeleteBackoff(ctx, "WI-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rows, _ := db.ListBackoff(ctx)
	if len(rows) != 0 {
		t.Fatalf("rows=%d want 0 after delete", len(rows))
	}
}

// TestScheduler_BackoffPurgeExpired drops entries whose retry_at < now (R-MEGA-2 P4).
func TestScheduler_BackoffPurgeExpired(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	old := state.BackoffEntry{WorkItemID: "OLD", RetryAt: now.Add(-time.Hour)}
	fresh := state.BackoffEntry{WorkItemID: "FRESH", RetryAt: now.Add(time.Hour)}
	if err := db.UpsertBackoff(ctx, old); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	if err := db.UpsertBackoff(ctx, fresh); err != nil {
		t.Fatalf("upsert fresh: %v", err)
	}
	n, err := db.PurgeExpiredBackoff(ctx, now)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 1 {
		t.Fatalf("purged=%d want 1", n)
	}
	rows, _ := db.ListBackoff(ctx)
	if len(rows) != 1 || rows[0].WorkItemID != "FRESH" {
		t.Fatalf("remaining=%+v want FRESH only", rows)
	}
}

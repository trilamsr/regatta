package state

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
)

// TestListEventsSince_TimeCutoffPushedIntoSQL asserts in-SQL cutoff returns the fresh row even when older rows exceed LIMIT (R5-Bug-1).
func TestListEventsSince_TimeCutoffPushedIntoSQL(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	db := newClockedTestDB(t, &now)
	ctx := context.Background()

	// 5 old rows at t0, 1 fresh row at t0+10min. Uses two distinct
	// canonical kinds so the test asserts the cutoff filter, not the
	// kind filter.
	for i := 0; i < 5; i++ {
		if err := db.RecordEvent(ctx, 0, string(obs.EventTickStarted), `{}`); err != nil {
			t.Fatalf("record old %d: %v", i, err)
		}
	}
	now = now.Add(10 * time.Minute)
	if err := db.RecordEvent(ctx, 0, string(obs.EventTickCompleted), `{}`); err != nil {
		t.Fatalf("record fresh: %v", err)
	}

	// Cutoff one minute before the fresh row, limit of 3 (smaller
	// than the 5 old rows). Without SQL-side cutoff, ListEvents
	// returns the 3 oldest rows; Go-side filter drops them
	// all and the fresh row is invisible.
	cutoffUnix := now.Add(-1 * time.Minute).Unix()
	got, err := db.ListEventsSince(ctx, 0, cutoffUnix, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (the fresh row inside cutoff)", len(got))
	}
	if got[0].Kind != string(obs.EventTickCompleted) {
		t.Fatalf("wrong row: kind=%q want %q", got[0].Kind, obs.EventTickCompleted)
	}
}

// TestListEventsByKindSinceTime_TimeCutoffPushedIntoSQL asserts the kind-filtered path also pushes cutoff into SQL (R5-Bug-1 second half).
func TestListEventsByKindSinceTime_TimeCutoffPushedIntoSQL(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	db := newClockedTestDB(t, &now)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := db.RecordEvent(ctx, 0, "secrets_rotated", `{}`); err != nil {
			t.Fatalf("record old %d: %v", i, err)
		}
	}
	now = now.Add(10 * time.Minute)
	if err := db.RecordEvent(ctx, 0, "secrets_rotated", `{"chain":"fresh"}`); err != nil {
		t.Fatalf("record fresh: %v", err)
	}

	cutoffUnix := now.Add(-1 * time.Minute).Unix()
	got, err := db.ListEventsByKindSinceTime(ctx, "secrets_rotated", 0, cutoffUnix, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].PayloadJSON != `{"chain":"fresh"}` {
		t.Fatalf("wrong row payload: %q", got[0].PayloadJSON)
	}
}

// TestListEventsSince_ZeroCutoffDisablesFilter asserts cutoffUnix=0 returns all rows past sinceID.
func TestListEventsSince_ZeroCutoffDisablesFilter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := db.RecordEvent(ctx, 0, string(obs.EventTickStarted), `{}`); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	got, err := db.ListEventsSince(ctx, 0, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
}

// TestListEventsByKindSinceTime_ZeroCutoffDisablesFilter asserts kind-filtered cutoffUnix=0 returns all matching rows.
func TestListEventsByKindSinceTime_ZeroCutoffDisablesFilter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.RecordEvent(ctx, 0, string(obs.EventTickStarted), `{}`); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if err := db.RecordEvent(ctx, 0, string(obs.EventTickCompleted), `{}`); err != nil {
		t.Fatalf("record b: %v", err)
	}
	got, err := db.ListEventsByKindSinceTime(ctx, string(obs.EventTickStarted), 0, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Kind != string(obs.EventTickStarted) {
		t.Fatalf("got %d events first.kind=%q, want 1 %q", len(got), got[0].Kind, obs.EventTickStarted)
	}
}

package state

import (
	"context"
	"testing"
	"time"
)

// TestListEventsSince_TimeCutoffPushedIntoSQL pins the R5-Bug-1 fix:
// without an in-SQL created_at filter, ListEvents ASC+LIMIT returns
// oldest rows and a Go-side cutoff drops them all. ListEventsSince
// must return the row inside the time window even when N>limit
// older rows precede it.
func TestListEventsSince_TimeCutoffPushedIntoSQL(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	db := newClockedTestDB(t, &now)
	ctx := context.Background()

	// 5 old rows at t0, 1 fresh row at t0+10min.
	for i := 0; i < 5; i++ {
		if err := db.RecordEvent(ctx, 0, "old", `{}`); err != nil {
			t.Fatalf("record old %d: %v", i, err)
		}
	}
	now = now.Add(10 * time.Minute)
	if err := db.RecordEvent(ctx, 0, "fresh", `{}`); err != nil {
		t.Fatalf("record fresh: %v", err)
	}

	// Cutoff one minute before the fresh row, limit of 3 (smaller
	// than the 5 old rows). Without SQL-side cutoff, ListEvents
	// returns the 3 oldest "old" rows; Go-side filter drops them
	// all and the fresh row is invisible.
	cutoffUnix := now.Add(-1 * time.Minute).Unix()
	got, err := db.ListEventsSince(ctx, 0, cutoffUnix, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (the fresh row inside cutoff)", len(got))
	}
	if got[0].Kind != "fresh" {
		t.Fatalf("wrong row: kind=%q want fresh", got[0].Kind)
	}
}

// TestListEventsByKindSinceTime_TimeCutoffPushedIntoSQL is the kind-filtered
// twin of the test above. Closes the reviewer-flagged second half of
// R5-Bug-1: `events tail --kind K --since DUR` had the same asymmetric
// cutoff trap as the no-kind path.
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

// TestListEventsSince_ZeroCutoffDisablesFilter pins the cutoffUnix=0
// semantic: callers passing 0 (events tail without --since) must get
// all rows past sinceID, ordered by id ascending.
func TestListEventsSince_ZeroCutoffDisablesFilter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := db.RecordEvent(ctx, 0, "k", `{}`); err != nil {
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

// TestListEventsByKindSinceTime_ZeroCutoffDisablesFilter mirrors the
// no-kind disable-filter pin for the kind-filtered path.
func TestListEventsByKindSinceTime_ZeroCutoffDisablesFilter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.RecordEvent(ctx, 0, "a", `{}`); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if err := db.RecordEvent(ctx, 0, "b", `{}`); err != nil {
		t.Fatalf("record b: %v", err)
	}
	got, err := db.ListEventsByKindSinceTime(ctx, "a", 0, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "a" {
		t.Fatalf("got %d events first.kind=%q, want 1 a", len(got), got[0].Kind)
	}
}

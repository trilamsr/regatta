package state

import (
	"context"
	"testing"
	"time"
)

// TestGetRun_TimeFieldsUTC asserts StartedAt + FinishedAt return UTC after DB round-trip; dashboard formatting relies on Location() == UTC.
func TestGetRun_TimeFieldsUTC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	finish := time.Unix(1717000300, 0)
	r := Run{
		ID:                  "run-utc-1",
		StartedAt:           time.Unix(1717000000, 0),
		FinishedAt:          &finish,
		Status:              "finished",
		DeclaredEffectClass: "filesystem-write",
	}
	if err := db.InsertRun(ctx, r); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	got, err := db.GetRun(ctx, "run-utc-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt.Location()=%v want UTC", got.StartedAt.Location())
	}
	if got.FinishedAt == nil {
		t.Fatalf("FinishedAt nil; want non-nil")
	}
	if got.FinishedAt.Location() != time.UTC {
		t.Errorf("FinishedAt.Location()=%v want UTC", got.FinishedAt.Location())
	}
}

// TestListRecentRuns_TimeFieldsUTC asserts StartedAt returns UTC for the list-path so the dashboard recent-runs table renders in a stable tz.
func TestListRecentRuns_TimeFieldsUTC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	if err := db.InsertRun(ctx, Run{ID: "run-utc-list-1", StartedAt: time.Unix(1717000000, 0), DeclaredEffectClass: "noop"}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	rows, err := db.ListRecentRuns(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentRuns: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("ListRecentRuns returned 0 rows")
	}
	for i, r := range rows {
		if r.StartedAt.Location() != time.UTC {
			t.Errorf("rows[%d].StartedAt.Location()=%v want UTC", i, r.StartedAt.Location())
		}
	}
}

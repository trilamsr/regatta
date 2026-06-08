package state

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// TestInsertRun_RoundTrip asserts every Run column survives an InsertRun + GetRun via cmp.Diff (#operator-console-S0).
func TestInsertRun_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	finish := time.Unix(1717000300, 0)
	rerunOf := "run-parent"
	want := Run{
		ID:                  "run-1",
		StartedAt:           time.Unix(1717000000, 0),
		FinishedAt:          &finish,
		Status:              "finished",
		SpecHash:            "sh",
		ModelHash:           "mh",
		PromptTemplateHash:  "pth",
		ToolImplHash:        "tih",
		Seed:                "seed-x",
		VersionsJSON:        `{"go":"1.25.0"}`,
		CausalHash:          "ch-x",
		RerunOf:             &rerunOf,
		TraceID:             "abcdef0123456789abcdef0123456789",
		DeclaredEffectClass: "filesystem-write+gh-mutation",
	}
	if err := db.InsertRun(ctx, want); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	got, err := db.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round-trip diff (-want +got):\n%s", diff)
	}
}

// TestInsertRun_DuplicateID asserts InsertRun fails when the same id reappears (#operator-console-S0).
func TestInsertRun_DuplicateID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	r := Run{ID: "run-1", StartedAt: time.Unix(1717000000, 0)}
	if err := db.InsertRun(ctx, r); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := db.InsertRun(ctx, r); err == nil {
		t.Error("expected duplicate-ID error, got nil")
	}
}

// TestGetRun_MissingID asserts GetRun surfaces an error when the row is absent (#operator-console-S0).
func TestGetRun_MissingID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.GetRun(ctx, "nonexistent"); err == nil {
		t.Error("expected error on missing ID")
	}
}

// TestListRecentRuns_OrderByStartedDesc asserts ListRecentRuns returns rows newest-first within limit (#operator-console-S0).
func TestListRecentRuns_OrderByStartedDesc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	rows := []Run{
		{ID: "r1", StartedAt: time.Unix(1717000000, 0)},
		{ID: "r2", StartedAt: time.Unix(1717000100, 0)},
		{ID: "r3", StartedAt: time.Unix(1717000200, 0)},
	}
	for _, r := range rows {
		if err := db.InsertRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListRecentRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d runs, want 3", len(got))
	}
	if got[0].ID != "r3" || got[2].ID != "r1" {
		t.Errorf("order: got %s..%s want r3..r1", got[0].ID, got[2].ID)
	}
}

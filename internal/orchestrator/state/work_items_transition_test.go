package state

import (
	"context"
	"errors"
	"testing"
)

// TestDB_TransitionWorkItem_HappyPath pins the typed CAS happy path.
func TestDB_TransitionWorkItem_HappyPath(t *testing.T) {
	ctx := context.Background()
	db := openOrphanTestDB(t)
	seedOrphanWI(t, db, "F-1", WorkStatusPlanned)

	if err := db.TransitionWorkItem(ctx, "F-1", WorkStatusPlanned, WorkStatusRejected); err != nil {
		t.Fatalf("TransitionWorkItem: %v", err)
	}
	wi, err := db.GetWorkItem(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status != WorkStatusRejected {
		t.Fatalf("status=%q; want %q", wi.Status, WorkStatusRejected)
	}
}

// TestDB_TransitionWorkItem_FromMismatchFails pins CAS-loses-race.
func TestDB_TransitionWorkItem_FromMismatchFails(t *testing.T) {
	ctx := context.Background()
	db := openOrphanTestDB(t)
	seedOrphanWI(t, db, "F-2", WorkStatusArchived)

	err := db.TransitionWorkItem(ctx, "F-2", WorkStatusPlanned, WorkStatusRejected)
	if !errors.Is(err, ErrInvalidWorkItemTransition) {
		t.Fatalf("err=%v; want ErrInvalidWorkItemTransition", err)
	}
	wi, gErr := db.GetWorkItem(ctx, "F-2")
	if gErr != nil {
		t.Fatalf("GetWorkItem: %v", gErr)
	}
	if wi.Status != WorkStatusArchived {
		t.Fatalf("status=%q; want unchanged %q", wi.Status, WorkStatusArchived)
	}
}

// TestDB_TransitionWorkItem_UnknownIDFails pins missing-row semantics.
func TestDB_TransitionWorkItem_UnknownIDFails(t *testing.T) {
	ctx := context.Background()
	db := openOrphanTestDB(t)

	err := db.TransitionWorkItem(ctx, "missing", WorkStatusPlanned, WorkStatusRejected)
	if !errors.Is(err, ErrInvalidWorkItemTransition) {
		t.Fatalf("err=%v; want ErrInvalidWorkItemTransition", err)
	}
}

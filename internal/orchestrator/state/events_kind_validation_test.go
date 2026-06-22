package state

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
)

// TestRecordEvent_RejectsUnknownKind asserts unknown kinds fail-closed (Bug S4).
func TestRecordEvent_RejectsUnknownKind(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	err := db.RecordEvent(ctx, 0, "spawn_compeleted", `{}`)
	if err == nil {
		t.Fatalf("RecordEvent accepted typo'd kind; want non-nil error")
	}
	if !strings.Contains(err.Error(), "unknown event kind") {
		t.Fatalf("RecordEvent err = %v; want substring \"unknown event kind\"", err)
	}
}

// TestRecordEvent_AcceptsKnownKind asserts canonical obs.EventName kinds pass (Bug S4).
func TestRecordEvent_AcceptsKnownKind(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.RecordEvent(ctx, 0, string(obs.EventTickStarted), `{}`); err != nil {
		t.Fatalf("RecordEvent rejected canonical kind %q: %v", obs.EventTickStarted, err)
	}
}

// TestRecordEventTx_RejectsUnknownKind asserts the tx variant fail-closes the same way (Bug S4).
func TestRecordEventTx_RejectsUnknownKind(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return db.RecordEventTx(ctx, tx, 0, "spawn_compeleted", `{}`)
	})
	if err == nil {
		t.Fatalf("RecordEventTx accepted typo'd kind; want non-nil error")
	}
	if !strings.Contains(err.Error(), "unknown event kind") {
		t.Fatalf("RecordEventTx err = %v; want substring \"unknown event kind\"", err)
	}
}

// TestRecordEventTx_AcceptsKnownKind asserts canonical obs.EventName kinds pass the tx variant (Bug S4).
func TestRecordEventTx_AcceptsKnownKind(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return db.RecordEventTx(ctx, tx, 0, string(obs.EventTickStarted), `{}`)
	})
	if err != nil {
		t.Fatalf("RecordEventTx rejected canonical kind %q: %v", obs.EventTickStarted, err)
	}
}

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

// TestIsKnownEvent_AllEventNamesPass asserts obs.AllEventNames registry stays in sync with IsKnownEvent.
func TestIsKnownEvent_AllEventNamesPass(t *testing.T) {
	for _, e := range obs.AllEventNames() {
		if !obs.IsKnownEvent(string(e)) {
			t.Errorf("obs.AllEventNames() entry %q fails IsKnownEvent — registry drift", e)
		}
	}
}

// legacyUnderscoreKinds mirrors obs.legacyUnderscoreEventNames; the obs slice is package-internal so a local copy keeps wire-string regressions loud here.
var legacyUnderscoreKinds = []string{
	"cost_cap_throttled",
	"cost_cap_resumed",
	"merge_intent",
	"merge_completed",
	"merge_executed",
	"merge_failed",
	"merge_recovered",
	"gate_rejected",
	"escalated",
	"labeled",
	"recovered_crashed",
	"reaped",
	"secrets_rotated",
	"agent_pr_opened",
	"agent_pr_head_changed",
	"agent_branch_renamed",
	"agent_pr_dirty",
}

// TestRecordEvent_AcceptsAllLegacyUnderscoreKinds asserts every wire-pinned legacy kind round-trips through RecordEvent.
func TestRecordEvent_AcceptsAllLegacyUnderscoreKinds(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, kind := range legacyUnderscoreKinds {
		if err := db.RecordEvent(ctx, 0, kind, `{}`); err != nil {
			t.Errorf("RecordEvent(%q) wire-pinned kind: got %v, want nil", kind, err)
		}
	}
}

// TestRecordEventTx_AcceptsAllLegacyUnderscoreKinds mirrors the round-trip assertion for the tx variant.
func TestRecordEventTx_AcceptsAllLegacyUnderscoreKinds(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, kind := range legacyUnderscoreKinds {
		err := db.WithTx(ctx, func(tx *sql.Tx) error {
			return db.RecordEventTx(ctx, tx, 0, kind, `{}`)
		})
		if err != nil {
			t.Errorf("RecordEventTx(%q) wire-pinned kind: got %v, want nil", kind, err)
		}
	}
}

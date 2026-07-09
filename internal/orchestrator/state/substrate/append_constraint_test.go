package substrate_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_OversizedPayloadWrappedGenericAndRollsBack pins payload > 1024B → generic "substrate: insert" wrap (not ErrReplay) + tx rollback.
func TestSubstrate_OversizedPayloadWrappedGenericAndRollsBack(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	substrate.ResetClockForTesting()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// 1200-byte work_item_id keeps the JSON payload > 1024B while
	// passing heartbeatPayload's non-empty check.
	big := strings.Repeat("x", 1200)
	payload := fmt.Sprintf(`{"work_item_id":%q,"timestamp":1}`, big)
	e := mkEvent(0xb1, "run-BIG", substrate.KindHeartbeat, payload, now)

	err := appendEventTx(ctx, t, db, e)
	if err == nil {
		t.Fatal("append oversized payload: err=nil, want CHECK failure")
	}
	if errors.Is(err, substrate.ErrReplay) {
		t.Fatalf("append oversized payload: err mis-classified as ErrReplay: %v", err)
	}
	if !strings.Contains(err.Error(), "substrate: insert") {
		t.Fatalf("append oversized payload: err=%v want wrap containing %q", err, "substrate: insert")
	}

	// Rollback confirmation: no row for run-BIG.
	events, ferr := substrate.Fold(ctx, db, "run-BIG", substrate.KindHeartbeat)
	if ferr != nil {
		t.Fatalf("Fold after failed insert: %v", ferr)
	}
	if len(events) != 0 {
		t.Fatalf("rollback broken: found %d rows for run-BIG after failed insert", len(events))
	}
}

// TestSubstrate_UnknownSupersedesTargetRejected pins unknown supersedes → cycleCheck "target not found" (not ErrReplay/ErrSupersedesCycle) + tx rollback (spec §8 R7).
func TestSubstrate_UnknownSupersedesTargetRejected(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	substrate.ResetClockForTesting()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	e := mkEvent(0xf1, "run-FK", substrate.KindHeartbeat,
		`{"work_item_id":"WI-FK","timestamp":1}`, now)
	// Point at a syntactically valid ULID that was never inserted.
	e.Supersedes = substrate.Mint(now.Add(-time.Hour))

	err := appendEventTx(ctx, t, db, e)
	if err == nil {
		t.Fatal("append unknown supersedes: err=nil, want target-not-found")
	}
	if errors.Is(err, substrate.ErrReplay) {
		t.Fatalf("append unknown supersedes: mis-classified as ErrReplay: %v", err)
	}
	if errors.Is(err, substrate.ErrSupersedesCycle) {
		t.Fatalf("append unknown supersedes: mis-classified as ErrSupersedesCycle: %v", err)
	}
	if !strings.Contains(err.Error(), "supersedes target") ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("append unknown supersedes: err=%v want message containing %q + %q",
			err, "supersedes target", "not found")
	}

	events, ferr := substrate.Fold(ctx, db, "run-FK", substrate.KindHeartbeat)
	if ferr != nil {
		t.Fatalf("Fold after failed insert: %v", ferr)
	}
	if len(events) != 0 {
		t.Fatalf("rollback broken: found %d rows for run-FK after failed insert", len(events))
	}
}

// TestSubstrate_WrittenByCheckConstraintWrappedGeneric pins empty written_by → schema CHECK → generic "substrate: insert" wrap (not ErrReplay).
func TestSubstrate_WrittenByCheckConstraintWrappedGeneric(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	substrate.ResetClockForTesting()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	e := mkEvent(0xc1, "run-CK", substrate.KindHeartbeat,
		`{"work_item_id":"WI-CK","timestamp":1}`, now)
	e.WrittenBy = "" // CHECK: length(written_by) > 0

	err := appendEventTx(ctx, t, db, e)
	if err == nil {
		t.Fatal("append empty written_by: err=nil, want CHECK failure")
	}
	if errors.Is(err, substrate.ErrReplay) {
		t.Fatalf("append empty written_by: mis-classified as ErrReplay: %v", err)
	}
	if !strings.Contains(err.Error(), "substrate: insert") {
		t.Fatalf("append empty written_by: err=%v want wrap containing %q", err, "substrate: insert")
	}

	events, ferr := substrate.Fold(ctx, db, "run-CK", substrate.KindHeartbeat)
	if ferr != nil {
		t.Fatalf("Fold after failed insert: %v", ferr)
	}
	if len(events) != 0 {
		t.Fatalf("rollback broken: found %d rows for run-CK after failed insert", len(events))
	}
}

// TestSubstrate_UniqueNonceStillMapsToErrReplay pins genuine UNIQUE-nonce collision still surfaces ErrReplay + tx rollback (regression guard for idempotent-retry callers).
func TestSubstrate_UniqueNonceStillMapsToErrReplay(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	substrate.ResetClockForTesting()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	first := mkEvent(0xd1, "run-UQ", substrate.KindHeartbeat,
		`{"work_item_id":"WI-UQ","timestamp":1}`, now)
	if err := appendEventTx(ctx, t, db, first); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Same (run, writer, nonce) with fresh ID + payload triggers UNIQUE.
	second := mkEvent(0xd1, "run-UQ", substrate.KindHeartbeat,
		`{"work_item_id":"WI-UQ","timestamp":2}`, now.Add(time.Millisecond))

	err := appendEventTx(ctx, t, db, second)
	if !errors.Is(err, substrate.ErrReplay) {
		t.Fatalf("UNIQUE collision: err=%v want ErrReplay", err)
	}
	// Confirm rollback: only the first row persists for run-UQ.
	events, ferr := substrate.Fold(ctx, db, "run-UQ", substrate.KindHeartbeat)
	if ferr != nil {
		t.Fatalf("Fold: %v", ferr)
	}
	if len(events) != 1 {
		t.Fatalf("rollback broken: want 1 surviving row, got %d", len(events))
	}
}


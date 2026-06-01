package substrate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_AppendEventRoundTrip pins append→fold→verify shape: payload survives, signature verifies, fold returns the event.
func TestSubstrate_AppendEventRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	e := mkEvent(0xaa, "run-A", substrate.KindHeartbeat,
		`{"work_item_id":"WI-1","timestamp":1}`, now)

	if err := appendEventTx(ctx, t, db, e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	events, err := substrate.Fold(ctx, db, "run-A", substrate.KindHeartbeat)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("fold got %d events, want 1", len(events))
	}
	got := events[0]
	if got.ID != e.ID || got.RunID != e.RunID || string(got.PayloadJSON) != `{"work_item_id":"WI-1","timestamp":1}` {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, e)
	}
	if err := substrate.Verify(got, testKeyring()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestSubstrate_ReplayProtection pins (run_id, written_by, nonce) UNIQUE collision ⇒ ErrReplay.
func TestSubstrate_ReplayProtection(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	first := mkEvent(0x11, "run-R", substrate.KindHeartbeat,
		`{"work_item_id":"WI-R","timestamp":1}`, now)
	if err := appendEventTx(ctx, t, db, first); err != nil {
		t.Fatalf("first append: %v", err)
	}

	// Second event: fresh ID + payload but same (run, writer, nonce).
	second := mkEvent(0x11, "run-R", substrate.KindHeartbeat,
		`{"work_item_id":"WI-R","timestamp":2}`, now.Add(time.Millisecond))
	err := appendEventTx(ctx, t, db, second)
	if !errors.Is(err, substrate.ErrReplay) {
		t.Fatalf("second append: err=%v want ErrReplay", err)
	}
}

// TestSubstrate_NonceMismatchRejected pins Verify rejects column-nonce ≠ signed-payload-nonce per spec §5 I5.
func TestSubstrate_NonceMismatchRejected(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	e := mkEvent(0x22, "run-N", substrate.KindHeartbeat,
		`{"work_item_id":"WI-N","timestamp":1}`, now)
	if err := substrate.Sign(&e, testKey, testKeyID); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// Tamper: rotate column-nonce after signing.
	e.Nonce = fixedNonce(0xff)

	err := substrate.Verify(e, testKeyring())
	if !substrate.IsUnverifiable(err) {
		t.Fatalf("Verify: err=%v want ErrUnverifiable", err)
	}
}

// TestSubstrate_SupersedesCycleRejected pins Kahn's cycle-check inside insert tx ⇒ ErrSupersedesCycle (self-loop case).
func TestSubstrate_SupersedesCycleRejected(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Insert two independent rows first (no cycle).
	a := mkEvent(0xa1, "run-C", substrate.KindHeartbeat,
		`{"work_item_id":"WI-C","timestamp":1}`, now)
	b := mkEvent(0xa2, "run-C", substrate.KindHeartbeat,
		`{"work_item_id":"WI-C","timestamp":2}`, now.Add(time.Millisecond))
	if err := appendEventTx(ctx, t, db, a); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if err := appendEventTx(ctx, t, db, b); err != nil {
		t.Fatalf("insert b: %v", err)
	}

	// Self-loop is the smallest possible cycle. Multi-node cycles
	// can't be constructed via AppendEvent alone — the FK requires
	// each new event's supersedes target to exist, so all manufactured
	// edges point backwards in insertion order, never forming a cycle.
	// T-S3's property test exercises the multi-node graph shapes by
	// injecting synthetic rows.
	self := mkEvent(0xa3, "run-C", substrate.KindHeartbeat,
		`{"work_item_id":"WI-C","timestamp":3}`, now.Add(2*time.Millisecond))
	self.Supersedes = self.ID // self-loop ⇒ trivial cycle
	err := appendEventTx(ctx, t, db, self)
	if !errors.Is(err, substrate.ErrSupersedesCycle) {
		t.Fatalf("self-loop append: err=%v want ErrSupersedesCycle", err)
	}
}

// TestSubstrate_CrossRunReplayRejected pins signature-binds-run_id (cross-run replay fails Verify) per spec §10 #1.
func TestSubstrate_CrossRunReplayRejected(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	e := mkEvent(0x33, "run-X", substrate.KindHeartbeat,
		`{"work_item_id":"WI-X","timestamp":1}`, now)
	if err := substrate.Sign(&e, testKey, testKeyID); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// "Replay" — mutate the run_id but keep the signature.
	e.RunID = "run-Y"

	err := substrate.Verify(e, testKeyring())
	if !substrate.IsUnverifiable(err) {
		t.Fatalf("Verify across run: err=%v want ErrUnverifiable", err)
	}
}

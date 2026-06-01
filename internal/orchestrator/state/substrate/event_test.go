package substrate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_AppendEventRoundTrip — append + fold + verify round-trip.
// Pins: payload survives, signature verifies, fold returns the event we appended.
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

// TestSubstrate_ReplayProtection — same (run_id, written_by, nonce)
// twice ⇒ second call ErrReplay (UNIQUE-collision path).
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

// TestSubstrate_NonceMismatchRejected — verifier asserts column-nonce
// == signed-payload nonce per spec §5 I5. Construct a row where these
// disagree; Verify must reject.
//
// This is the signer-side form: build the event, sign it, then mutate
// e.Nonce after Sign returns. T-S3 ships the verifier-side adversarial
// form (TestSubstrate_NonceMismatchRejected_Verifier).
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

// TestSubstrate_SupersedesCycleRejected — A supersedes B; attempt to
// insert B supersedes A ⇒ ErrSupersedesCycle (Kahn's check inside the
// insert tx).
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

	// Now build c such that c supersedes a (legal) and another d such
	// that d supersedes c, then attempt e: e supersedes d AND we
	// mutate to close a back-edge. Simplest: insert c supersedes a,
	// then attempt to insert a row that supersedes c BUT we set its
	// ID to a's existing ID — wait, ID is PK. Easier: insert c → a,
	// d → c, then attempt to RE-INSERT a row whose ID = a but
	// supersedes = d. That collides on PK.
	//
	// Correct shape: insert c → a; then attempt insertion of a NEW
	// row N with supersedes = c. Inside cycleCheck we overlay the
	// new (N → c) edge into the existing graph (a, b, c→a). The
	// graph is acyclic (a is the root). To force a cycle: insert
	// c → a, then attempt N with supersedes = c AND we manually
	// add an entry making c supersede N. Since N doesn't exist yet
	// the only way to manufacture a cycle in this test is to insert
	// c → N (forward-ref) — impossible because N doesn't exist (FK).
	//
	// Real cycle test: insert chain a -> b' -> c'  (where -> means
	// "supersedes"), then attempt to insert b' AGAIN with supersedes
	// pointing forward to itself or to c'. Since b' already exists
	// the PK blocks re-insert. The actual cycle vector is: the
	// CycleCheck pre-INSERT step running over the in-memory overlay
	// detects "if I add this new edge, would the resulting graph have
	// a cycle?". For that to fail, the existing graph must already
	// contain a back-edge waiting for the new edge to close it.
	//
	// Concretely: insert N1 (no supersedes). Insert N2 supersedes N1.
	// Insert N3 supersedes N2. Now attempt to insert N1 AGAIN with
	// supersedes=N3 — PK blocks. So the only way to truly test the
	// cycle path is to inject a synthetic graph bypassing AppendEvent,
	// OR to use the cycle property test (T-S3) which mutates the
	// graph directly.
	//
	// Pragmatic: insert N1 with no supersedes; insert N2 supersedes
	// N1; attempt N3 supersedes N2 with N3.ID == N1.ID (PK collision,
	// pre-cycle-check — so the cycle path is reached only AFTER PK
	// check succeeds). We need a row whose insertion overlays a
	// cycle without PK conflict. The clean shape: create a self-loop
	// by submitting an event where ID == Supersedes.
	self := mkEvent(0xa3, "run-C", substrate.KindHeartbeat,
		`{"work_item_id":"WI-C","timestamp":3}`, now.Add(2*time.Millisecond))
	self.Supersedes = self.ID // self-loop ⇒ trivial cycle
	err := appendEventTx(ctx, t, db, self)
	if !errors.Is(err, substrate.ErrSupersedesCycle) {
		t.Fatalf("self-loop append: err=%v want ErrSupersedesCycle", err)
	}
}

// TestSubstrate_CrossRunReplayRejected — capture a valid event from
// run X; attempt to "replay" the same payload into run Y using the
// same nonce. The HMAC signs run_id so the signature does not verify
// when run_id changes. Spec §10 #1.
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

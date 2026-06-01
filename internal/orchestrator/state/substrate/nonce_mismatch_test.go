package substrate_test

import (
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_NonceMismatchRejected_Verifier is the A+-tier
// adversarial form of spec §5 I5: an attacker who replays a signed
// event payload but rotates the column-nonce (e.g. to dodge the
// UNIQUE(run_id, written_by, nonce) index) must still fail Verify().
//
// Stricter than T-S1's TestSubstrate_NonceMismatchRejected: this test
// reconstructs the verifier-side path directly — Sign, mutate, Verify
// — without any AppendEvent dependency. Pinning the verifier in
// isolation locks the I5 invariant against future refactors of the
// signer that might inadvertently couple column-nonce to signed-nonce
// in a way the round-trip test cannot catch.
//
// Stages exercised:
//  1. Sign a baseline event (nonce=N1).
//  2. Mutate e.Nonce to N2 (the attacker's freshly-minted column nonce).
//  3. Verify must return ErrUnverifiable — the HMAC was computed over
//     the signed-payload nonce N1, but the canonical-JSON now contains
//     N2 ⇒ HMAC compare mismatches.
//
// If a future refactor introduces a column-vs-signed-nonce equality
// check before HMAC, this test still catches a regression: the equality
// check itself must trip the ErrUnverifiable sentinel.
func TestSubstrate_NonceMismatchRejected_Verifier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	e := mkEvent(0x42, "run-NV", substrate.KindHeartbeat,
		`{"work_item_id":"WI-NV","timestamp":1}`, now)

	// Baseline: sign at nonce N1.
	if err := substrate.Sign(&e, testKey, testKeyID); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	originalNonce := e.Nonce
	originalMAC := e.SigMAC

	// Attacker's move: rotate the column nonce to N2 while keeping the
	// signature bytes from the N1 signing. Real attackers couldn't
	// reproduce e.SigMAC for N2 without the key; this test simulates
	// what a packet they DID capture would look like if they tried to
	// reuse it with a fresh nonce column to dodge UNIQUE.
	e.Nonce = fixedNonce(0xCC)
	if e.Nonce == originalNonce {
		t.Fatalf("test bug: mutated nonce equals original")
	}
	if e.SigMAC != originalMAC {
		t.Fatalf("test bug: SigMAC mutated by nonce rotation")
	}

	err := substrate.Verify(e, testKeyring())
	if !substrate.IsUnverifiable(err) {
		t.Fatalf("Verify: err=%v want IsUnverifiable", err)
	}
}

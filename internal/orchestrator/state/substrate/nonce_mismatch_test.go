package substrate_test

import (
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_NonceMismatchRejected_Verifier pins spec §5 I5 A+-tier: verifier-side column-nonce rotation post-Sign ⇒ ErrUnverifiable.
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
	if !errors.Is(err, substrate.ErrUnverifiable) {
		t.Fatalf("Verify: err=%v want ErrUnverifiable", err)
	}
}

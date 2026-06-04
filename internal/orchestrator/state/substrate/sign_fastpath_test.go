package substrate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_SignCanonicalized_KeyMutationPoisoning pins R2 (#700): mutating a key buffer in place between calls must not yield a stale-cached HMAC state.
func TestSubstrate_SignCanonicalized_KeyMutationPoisoning(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"timestamp":1,"work_item_id":"WI-KM"}`)
	preCanon, err := canon.CanonicaliseJSON(payload)
	if err != nil {
		t.Fatalf("pre-canon: %v", err)
	}

	// Fixed Event values so MAC drift can only come from key state,
	// never from a ULID Mint roll.
	fixedEv := substrate.Event{
		ID:            "01J0000000000000000000000K",
		RunID:         "run-KM",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindHeartbeat,
		PayloadJSON:   payload,
		WrittenBy:     "tester",
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         fixedNonce(0xb0),
	}

	keyBuf := []byte("0123456789abcdef0123456789abcdef")
	e1 := fixedEv
	if err := substrate.SignCanonicalized(&e1, preCanon, keyBuf, "kid-A"); err != nil {
		t.Fatalf("first SignCanonicalized: %v", err)
	}
	firstMAC := e1.SigMAC

	// Mutate the key buffer in place. A pointer-identity cache misses
	// this and replays the prior HMAC padded-key state, producing a MAC
	// that does not match a fresh-buffer call against the same bytes.
	for i := range keyBuf {
		keyBuf[i] = 'z'
	}
	e2 := fixedEv
	if err := substrate.SignCanonicalized(&e2, preCanon, keyBuf, "kid-A"); err != nil {
		t.Fatalf("second SignCanonicalized: %v", err)
	}

	freshKey := []byte(strings.Repeat("z", 32))
	e3 := fixedEv
	if err := substrate.SignCanonicalized(&e3, preCanon, freshKey, "kid-A"); err != nil {
		t.Fatalf("third SignCanonicalized: %v", err)
	}

	if e2.SigMAC != e3.SigMAC {
		t.Fatalf("key-mutation-poisoning: mutated-buf MAC=%q fresh-buf MAC=%q (#700 R2)",
			e2.SigMAC, e3.SigMAC)
	}
	if firstMAC == e2.SigMAC {
		t.Fatalf("expected MAC drift after key mutation, got identical %q (#700 R2)", firstMAC)
	}
}

// TestSubstrate_SignCanonicalized_SigMACStableAcrossCalls pins R4 (#700): SigMAC retained across consecutive calls must not alias mutated state.
func TestSubstrate_SignCanonicalized_SigMACStableAcrossCalls(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"timestamp":1,"work_item_id":"WI-SM"}`)
	preCanon, err := canon.CanonicaliseJSON(payload)
	if err != nil {
		t.Fatalf("pre-canon: %v", err)
	}

	// Fixed Event values (no Mint) so the recomputed sign-call produces
	// the same envelope and the MAC compare is meaningful.
	mk := func(id string) substrate.Event {
		return substrate.Event{
			ID:            id,
			RunID:         "run-SM",
			TenantID:      substrate.DefaultTenantID,
			Kind:          substrate.KindHeartbeat,
			PayloadJSON:   payload,
			WrittenBy:     "tester",
			WrittenAt:     now.UnixMilli(),
			SchemaVersion: 1,
			Nonce:         fixedNonce(0xc0),
		}
	}

	ids := []string{
		"01J0000000000000000000000A",
		"01J0000000000000000000000B",
		"01J0000000000000000000000C",
		"01J0000000000000000000000D",
		"01J0000000000000000000000E",
	}
	macs := make([]string, len(ids))
	for i, id := range ids {
		e := mk(id)
		if err := substrate.SignCanonicalized(&e, preCanon, testKey, testKeyID); err != nil {
			t.Fatalf("SignCanonicalized[%d]: %v", i, err)
		}
		macs[i] = e.SigMAC
	}

	// Recompute the first row's MAC. If SigMAC aliased a reusable buffer
	// any subsequent call overwrote, macs[0] would have mutated.
	e0 := mk(ids[0])
	if err := substrate.SignCanonicalized(&e0, preCanon, testKey, testKeyID); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if macs[0] != e0.SigMAC {
		t.Fatalf("SigMAC[0] mutated after subsequent calls: stored=%q recomputed=%q (#700 R4)",
			macs[0], e0.SigMAC)
	}
	for i, m := range macs {
		if len(m) != 64 {
			t.Fatalf("SigMAC[%d] length=%d want 64 (#700 R4)", i, len(m))
		}
	}
}

// TestSubstrate_AppendEventCanonicalizedEquivalence_EscapedFields pins R6 (#700): fast-path MAC matches slow-path under JSON-special chars in BlobDigest/TraceID/Key.
func TestSubstrate_AppendEventCanonicalizedEquivalence_EscapedFields(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"work_item_id":"WI-ESC","timestamp":1}`)
	preCanon, err := canon.CanonicaliseJSON(payload)
	if err != nil {
		t.Fatalf("pre-canon: %v", err)
	}

	cases := []struct {
		name       string
		traceID    string
		blobDigest string
		key        string
	}{
		{
			name:       "quote_and_backslash",
			traceID:    `trace"with"quotes`,
			blobDigest: `digest\with\backslash`,
			key:        `key"with\both`,
		},
		{
			name:       "control_and_unicode",
			traceID:    "trace\nwith\tctrl",
			blobDigest: "digesté",
			key:        "key☃snow",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := mkEvent(0xdb, "run-ESC", substrate.KindHeartbeat, string(payload), now)
			base.TraceID = tc.traceID
			base.BlobDigest = tc.blobDigest
			base.Key = tc.key

			slow := base
			if err := substrate.Sign(&slow, testKey, testKeyID); err != nil {
				t.Fatalf("Sign slow: %v", err)
			}
			fast := base
			if err := substrate.SignCanonicalized(&fast, preCanon, testKey, testKeyID); err != nil {
				t.Fatalf("SignCanonicalized: %v", err)
			}
			if slow.SigMAC != fast.SigMAC {
				t.Fatalf("MAC drift on escaped fields: slow=%q fast=%q (#700 R6)",
					slow.SigMAC, fast.SigMAC)
			}
		})
	}
}

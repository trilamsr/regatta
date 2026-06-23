package schemas

import (
	"errors"
	"strings"
	"testing"
)

// TestVerifyMAC_TimingSafeOnHexLengthMismatch asserts a truncated hex mac surfaces as ErrUnverifiable rather than panicking on hex.Decode error (R-MEGA-3 LIVE-11).
func TestVerifyMAC_TimingSafeOnHexLengthMismatch(t *testing.T) {
	keyID := "k1"
	key := []byte("0123456789abcdef0123456789abcdef")
	keyring := map[string][]byte{keyID: key}

	payload := map[string]any{"hello": "world"}
	sig, err := Sign(payload, key, keyID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC[:30], // truncated → odd length → hex.DecodeString errors
	}
	if err := Verify(payload, keyring); !errors.Is(err, ErrUnverifiable) {
		t.Errorf("want ErrUnverifiable on truncated mac; got %v", err)
	}
}

// TestVerifyMAC_RejectsNonHexMac asserts a non-hex mac fails closed (R-MEGA-3 LIVE-11).
func TestVerifyMAC_RejectsNonHexMac(t *testing.T) {
	keyID := "k1"
	key := []byte("0123456789abcdef0123456789abcdef")
	keyring := map[string][]byte{keyID: key}

	payload := map[string]any{"hello": "world"}
	sig, err := Sign(payload, key, keyID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    strings.Repeat("Z", 64),
	}
	if err := Verify(payload, keyring); !errors.Is(err, ErrUnverifiable) {
		t.Errorf("want ErrUnverifiable on non-hex mac; got %v", err)
	}
}

// TestVerifyMAC_RoundTrip asserts the post-LIVE-11 raw-bytes compare still admits valid signatures (regression guard).
func TestVerifyMAC_RoundTrip(t *testing.T) {
	keyID := "k1"
	key := []byte("0123456789abcdef0123456789abcdef")
	keyring := map[string][]byte{keyID: key}

	payload := map[string]any{"hello": "world", "n": float64(42)}
	sig, err := Sign(payload, key, keyID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}
	if err := Verify(payload, keyring); err != nil {
		t.Errorf("VerifyAny round-trip failed: %v", err)
	}
}

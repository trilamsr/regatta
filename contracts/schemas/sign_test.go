package schemas

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCanonicalJSONStability(t *testing.T) {
	t.Parallel()
	v1 := map[string]any{
		"b": 2,
		"a": 1,
		"c": []any{3, 1, 2},
	}
	v2 := map[string]any{
		"c": []any{3, 1, 2},
		"a": 1,
		"b": 2,
	}
	c1, err := CanonicalJSON(v1)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := CanonicalJSON(v2)
	if err != nil {
		t.Fatal(err)
	}
	if string(c1) != string(c2) {
		t.Fatalf("canonical forms differ:\n%s\n%s", c1, c2)
	}
	want := `{"a":1,"b":2,"c":[3,1,2]}`
	if string(c1) != want {
		t.Fatalf("canonical=%s, want %s", c1, want)
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	key := []byte("a-secret-key-for-testing-only-32")
	keyID := "k1"
	keyring := map[string][]byte{keyID: key}

	payload := map[string]any{
		"program_id":   "m-000000000001",
		"feature_id":   "F-AUTH-01",
		"success_state": "success",
	}

	sig, err := Sign(payload, key, keyID)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Alg != "HMAC-SHA256" || sig.KeyID != keyID || len(sig.MAC) != 64 {
		t.Fatalf("bad signature: %+v", sig)
	}

	// Round-trip through JSON to simulate the wire shape.
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}
	raw, _ := json.Marshal(payload)
	var roundTripped map[string]any
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatal(err)
	}

	if err := Verify(roundTripped, keyring); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	t.Parallel()
	key := []byte("a-secret-key-for-testing-only-32")
	keyID := "k1"
	keyring := map[string][]byte{keyID: key}

	payload := map[string]any{
		"program_id":    "m-000000000001",
		"success_state": "success",
	}
	sig, _ := Sign(payload, key, keyID)
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}

	// Tamper: flip a field after signing.
	payload["success_state"] = "failure"

	err := Verify(payload, keyring)
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("expected ErrUnverifiable, got %v", err)
	}
}

func TestVerifyRejectsUnknownKey(t *testing.T) {
	t.Parallel()
	keyring := map[string][]byte{"k1": []byte("x")}
	payload := map[string]any{
		"signature": map[string]any{"alg": "HMAC-SHA256", "key_id": "k2", "mac": "aa"},
	}
	err := Verify(payload, keyring)
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("expected ErrUnverifiable, got %v", err)
	}
}

func TestVerifyRejectsUnsupportedAlg(t *testing.T) {
	t.Parallel()
	keyring := map[string][]byte{"k1": []byte("x")}
	payload := map[string]any{
		"signature": map[string]any{"alg": "MD5", "key_id": "k1", "mac": "aa"},
	}
	err := Verify(payload, keyring)
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("expected ErrUnverifiable, got %v", err)
	}
}

func TestSignRejectsWeakKey(t *testing.T) {
	t.Parallel()
	short := make([]byte, MinKeyLen-1)
	_, err := Sign(map[string]any{"x": 1}, short, "k1")
	if !errors.Is(err, ErrWeakKey) {
		t.Fatalf("expected ErrWeakKey, got %v", err)
	}
}

func TestVerifyWithAllowlistRejectsKeyOutsideList(t *testing.T) {
	t.Parallel()
	key := make([]byte, MinKeyLen)
	for i := range key {
		key[i] = 'a'
	}
	payload := map[string]any{"x": 1}
	sig, err := Sign(payload, key, "k1")
	if err != nil {
		t.Fatal(err)
	}
	payload["signature"] = map[string]any{"alg": sig.Alg, "key_id": sig.KeyID, "mac": sig.MAC}
	keyring := map[string][]byte{"k1": key}
	err = VerifyWithAllowlist(payload, keyring, map[string]bool{"k2": true})
	if !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("expected ErrUnknownKeyID, got %v", err)
	}
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("ErrUnknownKeyID should wrap ErrUnverifiable, got %v", err)
	}
}

func TestVerifyRejectsMissingSignature(t *testing.T) {
	t.Parallel()
	err := Verify(map[string]any{"x": 1}, nil)
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("expected ErrUnverifiable, got %v", err)
	}
}

// TestSign_KidInMAC_PreventsCrossKidForgery proves the MAC binds to
// key_id. Without binding, two keyring entries sharing the same key
// bytes under different kids cross-verify: a payload signed under
// kid-a accepts a swapped envelope key_id=kid-b.
func TestSign_KidInMAC_PreventsCrossKidForgery(t *testing.T) {
	t.Parallel()
	sharedKey := []byte("a-secret-key-for-testing-only-32")
	keyring := map[string][]byte{
		"kid-a": sharedKey,
		"kid-b": sharedKey,
	}

	payload := map[string]any{
		"program_id":    "m-000000000001",
		"success_state": "success",
	}
	sig, err := Sign(payload, sharedKey, "kid-a")
	if err != nil {
		t.Fatal(err)
	}
	// Envelope says kid-b but MAC was computed under kid-a's domain.
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": "kid-b",
		"mac":    sig.MAC,
	}

	if err := Verify(payload, keyring); !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("cross-kid forgery accepted; want ErrUnverifiable, got %v", err)
	}
}

// TestVerify_RejectsNonStringSignatureFields proves Verify fails fast
// when alg / key_id / mac arrive as non-string values. Pre-fix, the
// silent .(string) type assertion coerced to "" and HMAC compared
// against empty — a security-adjacent silent miss.
func TestVerify_RejectsNonStringSignatureFields(t *testing.T) {
	t.Parallel()
	key := []byte("a-secret-key-for-testing-only-32")
	keyring := map[string][]byte{"k1": key}

	cases := []struct {
		name string
		sig  map[string]any
	}{
		{"alg-not-string", map[string]any{"alg": 1, "key_id": "k1", "mac": "deadbeef"}},
		{"key_id-not-string", map[string]any{"alg": SigAlg, "key_id": 7, "mac": "deadbeef"}},
		{"mac-not-string", map[string]any{"alg": SigAlg, "key_id": "k1", "mac": 42}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{"x": 1, "signature": tc.sig}
			err := Verify(payload, keyring)
			if !errors.Is(err, ErrUnverifiable) {
				t.Fatalf("want ErrUnverifiable, got %v", err)
			}
			if err == nil || !strContains(err.Error(), "not a string") {
				t.Fatalf("want %q in error, got %v", "not a string", err)
			}
		})
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

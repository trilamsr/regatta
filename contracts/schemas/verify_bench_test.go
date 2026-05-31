package schemas

import (
	"fmt"
	"strings"
	"testing"
)

// buildPayload returns a signed payload whose canonical body is at
// least targetBytes long. The payload shape mirrors a real brief —
// nested map + array — so the canonicaliser exercises the same
// recursion the production HMAC path does.
func buildPayload(b *testing.B, targetBytes int, key []byte) map[string]any {
	b.Helper()
	chunk := strings.Repeat("x", 256)
	var items []any
	for size := 0; size < targetBytes; size += 256 + 32 {
		items = append(items, map[string]any{
			"id":      fmt.Sprintf("F-%05d", len(items)),
			"payload": chunk,
		})
	}
	payload := map[string]any{
		"program_id": "m-000000000001",
		"features":   items,
	}
	sig, err := Sign(payload, key, "k1")
	if err != nil {
		b.Fatalf("Sign: %v", err)
	}
	payload["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}
	return payload
}

// BenchmarkVerify times HMAC verify over canonicalised payloads at the
// 10KB / 100KB / 1MB boundaries. canonicalize() is doing most of the
// work below ~10KB; HMAC dominates above 1MB.
func BenchmarkVerify(b *testing.B) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	keyring := map[string][]byte{"k1": key}
	sizes := []struct {
		name  string
		bytes int
	}{
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}
	for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			payload := buildPayload(b, s.bytes, key)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := Verify(payload, keyring); err != nil {
					b.Fatalf("Verify: %v", err)
				}
			}
		})
	}
}

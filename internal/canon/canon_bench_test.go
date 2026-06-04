package canon

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// buildPayload returns a JSON byte slice approximately targetBytes
// long, shaped like an outputs-journal entry: nested map + repeated
// array elements. Key order is intentionally non-sorted so the
// canonicaliser does real work on every key.
func buildPayload(b *testing.B, targetBytes int) []byte {
	b.Helper()
	chunk := strings.Repeat("x", 200)
	var items []map[string]any
	for size := 0; size < targetBytes; size += 256 {
		items = append(items, map[string]any{
			"zz_id":   fmt.Sprintf("F-%05d", len(items)),
			"aa_kind": "feature",
			"mm_body": chunk,
		})
	}
	root := map[string]any{
		"zz_meta":  map[string]any{"version": 2, "schema": "outputs.v1"},
		"aa_items": items,
	}
	raw, err := json.Marshal(root)
	if err != nil {
		b.Fatalf("marshal: %v", err)
	}
	return raw
}

// BenchmarkCanonicaliseJSON times the canonicaliser at 1KB/10KB/100KB size boundaries.
func BenchmarkCanonicaliseJSON(b *testing.B) {
	sizes := []struct {
		name  string
		bytes int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}
	for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			payload := buildPayload(b, s.bytes)
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := CanonicaliseJSON(payload); err != nil {
					b.Fatalf("CanonicaliseJSON: %v", err)
				}
			}
		})
	}
}

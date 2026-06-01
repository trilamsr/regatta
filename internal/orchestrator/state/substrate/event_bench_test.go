package substrate_test

import (
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// BenchmarkAppendEvent_HMACAndCanonOnly is Task 0 perf gate per spec §7 (target ≤ 500 ns/op).
func BenchmarkAppendEvent_HMACAndCanonOnly(b *testing.B) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	e := substrate.Event{
		ID:            "01J0000000000000000000000A",
		RunID:         "run-B",
		WorkItemID:    "WI-B",
		TenantID:      substrate.DefaultTenantID,
		TraceID:       "",
		SpanID:        "",
		Kind:          substrate.KindHeartbeat,
		Key:           "",
		PayloadJSON:   []byte(`{"work_item_id":"WI-B","timestamp":1}`),
		BlobDigest:    "",
		Supersedes:    "",
		WrittenBy:     "bench",
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "00112233445566778899aabbccddeeff",
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := substrate.Sign(&e, key, "bench-key-1"); err != nil {
			b.Fatalf("Sign: %v", err)
		}
	}
}

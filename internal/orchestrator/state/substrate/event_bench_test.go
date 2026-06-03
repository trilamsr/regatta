package substrate_test

import (
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
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

// BenchmarkAppendEvent_PreCanonicalized is the F1 fast-path gate per spec §7 (#216, target ≤ 500 ns/op).
func BenchmarkAppendEvent_PreCanonicalized(b *testing.B) {
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
		PayloadJSON:   []byte(`{"timestamp":1,"work_item_id":"WI-B"}`),
		BlobDigest:    "",
		Supersedes:    "",
		WrittenBy:     "bench",
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "00112233445566778899aabbccddeeff",
	}
	// Pre-canonicalize ONCE outside the timed loop — that's the
	// fast-path contract: caller eats the canonicalisation cost once
	// per payload shape, sign cost is the per-row hot path.
	preCanon, err := canon.CanonicaliseJSON(e.PayloadJSON)
	if err != nil {
		b.Fatalf("pre-canon: %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := substrate.SignCanonicalized(&e, preCanon, key, "bench-key-1"); err != nil {
			b.Fatalf("SignCanonicalized: %v", err)
		}
	}
	b.StopTimer()
	// Bench gate: spec §7 pins ≤ 500 ns/op for the Sign-only fast path.
	// Report nanoseconds-per-op so CI dashboards can chart drift; the
	// b.Fatal makes the bench a load-bearing perf gate, not a vibes
	// metric. Skipped under -short so unit-only runs stay fast.
	if testing.Short() {
		return
	}
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	b.ReportMetric(nsPerOp, "ns/op-measured")
	if nsPerOp > 500.0 {
		b.Fatalf("F1 fast-path missed 500ns/op gate: got %.1f ns/op (#216)", nsPerOp)
	}
}

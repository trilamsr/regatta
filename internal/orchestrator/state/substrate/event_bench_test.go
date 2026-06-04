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
}

// TestSubstrate_AppendEventCanonicalizedHitsBudget runs the F1 fast-path bench programmatically and pins the spec §7 ≤500 ns/op target with x86-CI noise headroom (#216).
func TestSubstrate_AppendEventCanonicalizedHitsBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("perf gate skipped under -short")
	}
	r := testing.Benchmark(BenchmarkAppendEvent_PreCanonicalized)
	if r.N == 0 {
		t.Fatalf("bench did not run: %+v", r)
	}
	nsPerOp := float64(r.NsPerOp())
	t.Logf("BenchmarkAppendEvent_PreCanonicalized: %.1f ns/op, %d B/op, %d allocs/op",
		nsPerOp, r.AllocedBytesPerOp(), r.AllocsPerOp())
	// Spec §7 target is 500 ns/op on GitHub Actions ubuntu-latest 4-vCPU
	// x86_64. Local M1 Max measures 450-500 ns/op; gate at 1200 ns/op
	// catches ~2.4× regression while absorbing shared-runner + short-
	// benchtime noise (#216, tightened #700 R5 from 1500 — the R2/R4
	// correctness fixes added one extra alloc, so a strict 1000 ns/op
	// gate flaps under `testing.Benchmark`'s 1s default budget).
	if nsPerOp > 1200.0 {
		t.Fatalf("F1 fast-path regressed past 1200ns/op gate: got %.1f ns/op (#216)", nsPerOp)
	}
}

// File-based bench harness for issue #628 (OBS-B T2 deferred). The
// existing BenchmarkAppendEvent_HMACAndCanonOnly measures Sign() in
// isolation — real production has the chain-break Sweeper running
// concurrently against the same sqlite file. This bench pre-populates a
// chain on disk and runs the Sweeper in a goroutine while measuring
// Append latency, surfacing the writer/reader WAL-lock contention the
// pure-Sign bench cannot see.
//
// Hermetic by construction — the temp DB lands under b.TempDir() so
// each run is a fresh chain. The harness reuses the standard test
// keyring + AppendEvent path so the latency numbers reflect the same
// hot path as production.

package substrate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// BenchmarkSubstrate_AppendUnderSweeperLoad measures Append latency with the chain-break Sweeper running concurrently (#628).
func BenchmarkSubstrate_AppendUnderSweeperLoad(b *testing.B) {
	raw, dbPath := statetest.OpenMigratedRawBench(b)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Pre-populate ~5k rows so the Sweeper has a non-empty window.
	// One tx per 100 rows keeps the loop fast without merging into a
	// single mega-tx (sqlite's writer would otherwise serialise all
	// appends and hide the WAL-lock cost the bench is hunting).
	const preRows = 5000
	const txBatch = 100
	for i := 0; i < preRows; i += txBatch {
		tx, err := raw.BeginTx(ctx, nil)
		if err != nil {
			b.Fatalf("BeginTx: %v", err)
		}
		for j := 0; j < txBatch && i+j < preRows; j++ {
			e := newBenchEvent(b, fmt.Sprintf("pre-run-%d-%d", i, j), now.Add(time.Duration(i+j)*time.Microsecond))
			if err := substrate.AppendEvent(ctx, tx, e, testKey, testKeyID); err != nil {
				_ = tx.Rollback()
				b.Fatalf("AppendEvent pre-populate: %v", err)
			}
		}
		if err := tx.Commit(); err != nil {
			b.Fatalf("Commit pre-populate: %v", err)
		}
	}

	// Start the Sweeper with an aggressive interval so it actively
	// competes for the WAL during the measurement loop. 10ms ticks +
	// 1ms inter-batch pause is the worst-case shape we want bounded.
	sweeper, err := substrate.NewSweeper(substrate.SweeperConfig{
		DBPath:          dbPath,
		Keyring:         testKeyring(),
		Interval:        10 * time.Millisecond,
		Window:          24 * time.Hour,
		BatchSize:       500,
		InterBatchPause: time.Millisecond,
		Now:             func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		b.Fatalf("NewSweeper: %v", err)
	}
	sweeper.Start(ctx)
	b.Cleanup(func() { _ = sweeper.Close() })

	// Give the sweeper one tick to enter its read loop so the bench
	// timer starts inside the contended state — otherwise the first
	// few iters measure an idle DB.
	time.Sleep(15 * time.Millisecond)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := raw.BeginTx(ctx, nil)
		if err != nil {
			b.Fatalf("BeginTx: %v", err)
		}
		e := newBenchEvent(b, fmt.Sprintf("bench-run-%d", i), now.Add(time.Hour+time.Duration(i)*time.Microsecond))
		if err := substrate.AppendEvent(ctx, tx, e, testKey, testKeyID); err != nil {
			_ = tx.Rollback()
			b.Fatalf("AppendEvent under load: %v", err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatalf("Commit under load: %v", err)
		}
	}
}

// newBenchEvent builds a valid substrate event the bench can append
// repeatedly without tripping the UNIQUE(run_id, written_by, nonce)
// replay guard — each event carries a fresh random nonce.
func newBenchEvent(b *testing.B, runID string, at time.Time) substrate.Event {
	b.Helper()
	var n [16]byte
	if _, err := rand.Read(n[:]); err != nil {
		b.Fatalf("rand nonce: %v", err)
	}
	return substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindHeartbeat,
		PayloadJSON:   []byte(`{"work_item_id":"WI-B","timestamp":1}`),
		WrittenBy:     "bench",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         hex.EncodeToString(n[:]),
	}
}

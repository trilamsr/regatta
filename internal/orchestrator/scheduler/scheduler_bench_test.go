package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// newBenchDB opens a fresh sqlite-backed DB for the lifetime of a sub-
// benchmark. Each sub-bench gets its own temp file to keep state
// isolated; reusing a DB across sub-benches would amortise UpsertPending
// cost into the wrong N bucket.
func newBenchDB(b *testing.B) *state.DB {
	b.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(b.TempDir(), "s.db")))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func seedPlannedBench(b *testing.B, db *state.DB, id, lane string) {
	b.Helper()
	w := state.WorkItem{
		ID: id, Kind: state.KindFeature, Title: id,
		Lane: lane, Status: state.WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, time.Unix(1_700_000_000, 0)); err != nil {
		b.Fatalf("seed %s: %v", id, err)
	}
}

// BenchmarkTick measures the cost of a single scheduler.Tick over a
// pool of independent planned work items (no deps, no edges). At N=1000
// this exercises ListSpawnable + N reservation txs (each tx wraps
// UpsertPendingTx + TryAcquireLocksTx + TransitionAgentTx as one
// commit per issue #88), then steady-state ListSpawnable scans for
// subsequent ticks — the operator-visible Tick budget.
func BenchmarkTick(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db := newBenchDB(b)
			for i := 0; i < n; i++ {
				seedPlannedBench(b, db, fmt.Sprintf("F-%05d", i), "server")
			}
			sch := New(db, Config{LockTTL: time.Minute})
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := sch.Tick(ctx); err != nil {
					b.Fatalf("Tick: %v", err)
				}
			}
		})
	}
}

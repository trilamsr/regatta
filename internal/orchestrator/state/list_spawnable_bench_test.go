package state_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newSpawnableBenchDB(b *testing.B) *state.DB {
	b.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(b.TempDir(), "sp.db")))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

// seedSpawnableFixture writes n planned features. Every feature at index
// i (i >= chainEvery) depends on feature i-chainEvery, giving us a
// tunable inbound-edge density without producing a cycle. Edge rows are
// also created for the same dep pairs so the inbound-edge disjunct in
// ListSpawnable's WHERE has rows to scan.
func seedSpawnableFixture(b *testing.B, db *state.DB, n, chainEvery int) {
	b.Helper()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)

	for i := 0; i < n; i++ {
		var deps []string
		if i >= chainEvery {
			deps = []string{fmt.Sprintf("F-%05d", i-chainEvery)}
		}
		status := state.WorkStatusPlanned
		if i%chainEvery == 0 && i > 0 {
			status = state.WorkStatusMerged
		}
		w := state.WorkItem{
			ID: fmt.Sprintf("F-%05d", i), Kind: state.KindFeature,
			Title: fmt.Sprintf("F-%05d", i), Lane: "server", Status: status,
			DependsOnFeatures: deps,
		}
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, at); err != nil {
			b.Fatalf("seed %d: %v", i, err)
		}
	}

	// Materialise the same deps as work_item_edges so ListSpawnable's
	// edge disjunct has rows to scan. Without this the inbound-edge
	// branch short-circuits to "no edges" — accurate for v1 briefs but
	// unrealistic for the MVP-2 conditional-DAG workload this bench
	// tracks.
	var rows []state.EdgeRow
	for i := chainEvery; i < n; i++ {
		rows = append(rows, state.EdgeRow{
			ProgramID: "m-bench",
			FromID:    fmt.Sprintf("F-%05d", i-chainEvery),
			ToID:      fmt.Sprintf("F-%05d", i),
			OnSkip:    "cascade",
		})
	}
	if len(rows) > 0 {
		if err := db.UpsertEdgesAt(ctx, "m-bench", rows, at); err != nil {
			b.Fatalf("UpsertEdges: %v", err)
		}
	}

	// Flip half the edges to fired='true' so the inbound-edge disjunct
	// produces non-empty results. One UPDATE rather than re-walking
	// MarkEdgeFired keeps bench setup cheap (the MarkEdgeFired contract
	// is covered by work_items_query_test).
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE work_item_edges SET fired='true', fired_against='bench' WHERE id % 2 = 0`); err != nil {
		b.Fatalf("flip fired: %v", err)
	}
}

// BenchmarkListSpawnable times the join-based spawnable query at the
// sizes scheduler.Tick consumes it. The fixture mixes merged + planned
// rows plus inbound edges so the WHERE's three-way disjunct (no edges
// / fired=true / default-next escape) all see realistic input.
func BenchmarkListSpawnable(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db := newSpawnableBenchDB(b)
			seedSpawnableFixture(b, db, n, 5)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := db.ListSpawnable(ctx); err != nil {
					b.Fatalf("ListSpawnable: %v", err)
				}
			}
		})
	}
}

package state_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

func newBenchDB(b *testing.B) *state.DB { return statetest.OpenBenchDB(b) }

// seedDenseDAG populates work_items with a layered DAG of `depth` levels
// where each layer has `fanout` features. Every feature at layer L+1
// depends on EVERY feature at layer L, giving the dense case CycleCheck
// has to walk on every BriefLoader child upsert.
//
// Returns the candidate node whose dependencies span the deepest layer
// — the worst-case input for CycleCheck because reachable() must
// traverse the full graph before concluding "no back edge".
func seedDenseDAG(b *testing.B, db *state.DB, depth, fanout int) state.WorkItem {
	b.Helper()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)

	var prevLayer []string
	for layer := 0; layer < depth; layer++ {
		var thisLayer []string
		for i := 0; i < fanout; i++ {
			id := fmt.Sprintf("F-L%02d-%03d", layer, i)
			w := state.WorkItem{
				ID: id, Kind: state.KindFeature, Title: id,
				Lane: "server", Status: state.WorkStatusPlanned,
				DependsOnFeatures: append([]string{}, prevLayer...),
			}
			if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, at); err != nil {
				b.Fatalf("seed %s: %v", id, err)
			}
			thisLayer = append(thisLayer, id)
		}
		prevLayer = thisLayer
	}

	return state.WorkItem{
		ID: "F-CANDIDATE", Kind: state.KindFeature, Title: "cand",
		Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: append([]string{}, prevLayer...),
	}
}

// BenchmarkCycleCheck measures CycleCheck against dense layered DAGs.
// shape is (depth, fanout): a 10x10 DAG has 100 existing nodes and 100
// incoming dep edges on the candidate — the worst case for the
// scan-then-DFS approach.
func BenchmarkCycleCheck(b *testing.B) {
	shapes := []struct {
		depth, fanout int
	}{
		{5, 5},
		{10, 10},
	}
	for _, s := range shapes {
		name := fmt.Sprintf("depth=%d/fanout=%d", s.depth, s.fanout)
		b.Run(name, func(b *testing.B) {
			db := newBenchDB(b)
			cand := seedDenseDAG(b, db, s.depth, s.fanout)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := db.CycleCheck(ctx, cand); err != nil {
					b.Fatalf("CycleCheck: %v", err)
				}
			}
		})
	}
}

// seedLargeDAG seeds `nodes` work_items as a linear backbone (F-{i}
// depends on F-{i-1}) plus power-of-two cross edges so total edges
// land near edgesPerNode * nodes. Candidate depends on the chain tip,
// forcing traversal of every node before concluding "no cycle" — the
// worst case for any cycle-detection algorithm. Direction (higher
// index depends on lower) keeps the graph acyclic by construction.
func seedLargeDAG(b *testing.B, db *state.DB, nodes, edgesPerNode int) state.WorkItem {
	b.Helper()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)

	for i := 0; i < nodes; i++ {
		id := fmt.Sprintf("F-%07d", i)
		var deps []string
		if i > 0 {
			deps = append(deps, fmt.Sprintf("F-%07d", i-1))
		}
		stride := 2
		for len(deps) < edgesPerNode && stride <= i {
			deps = append(deps, fmt.Sprintf("F-%07d", i-stride))
			stride *= 2
		}
		w := state.WorkItem{
			ID: id, Kind: state.KindFeature, Title: id,
			Lane: "server", Status: state.WorkStatusPlanned,
			DependsOnFeatures: deps,
		}
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, at); err != nil {
			b.Fatalf("seed %s: %v", id, err)
		}
	}

	return state.WorkItem{
		ID: "F-CANDIDATE", Kind: state.KindFeature, Title: "cand",
		Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: []string{fmt.Sprintf("F-%07d", nodes-1)},
	}
}

// BenchmarkCycleCheck_LargeGraph: 10k items, ~50k edges (issue #90 acceptance fixture).
func BenchmarkCycleCheck_LargeGraph(b *testing.B) {
	db := newBenchDB(b)
	cand := seedLargeDAG(b, db, 10_000, 5)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.CycleCheck(ctx, cand); err != nil {
			b.Fatalf("CycleCheck: %v", err)
		}
	}
}

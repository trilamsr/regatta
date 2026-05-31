package state_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestListSpawnable_PropertyTopologicalReady (Grade-A DoD per spec §6):
// for any DAG of n≤8 nodes, ListSpawnable returns exactly the set of
// nodes whose dependencies are all in status='merged'. We generate
// random DAGs, mark a random subset as merged, then assert the
// returned set equals the topological-ready subset of planned nodes.
func TestListSpawnable_PropertyTopologicalReady(t *testing.T) {
	tmp := t.TempDir()
	var checkID atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		// Allow edges only from higher index to lower (guaranteed acyclic).
		// Node 0 has no possible predecessors; SampledFrom panics on
		// empty input so we explicitly leave it dep-less.
		deps := make(map[string][]string, n)
		for i := 0; i < n; i++ {
			possible := nodes[:i]
			if len(possible) == 0 {
				deps[nodes[i]] = nil
				continue
			}
			pick := rapid.SliceOfN(rapid.SampledFrom(possible), 0, len(possible)).Draw(rt, "deps_"+nodes[i])
			deps[nodes[i]] = dedupe(pick)
		}
		merged := map[string]bool{}
		for _, n := range nodes {
			if rapid.Bool().Draw(rt, "merged_"+n) {
				merged[n] = true
			}
		}

		// rapid.T has no TempDir; share the outer t.TempDir() and
		// give each check its own filename so parallel shrinking
		// passes can't collide on a single sqlite file.
		dbPath := filepath.Join(tmp, fmt.Sprintf("p-%d.db", checkID.Add(1)))
		db, err := state.Open(context.Background(), state.DSN(dbPath))
		if err != nil {
			rt.Fatalf("Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
		for _, id := range nodes {
			status := state.WorkStatusPlanned
			if merged[id] {
				status = state.WorkStatusMerged
			}
			w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id,
				Lane: "server", Status: status, DependsOnFeatures: deps[id]}
			if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, now); err != nil {
				rt.Fatalf("upsert %s: %v", id, err)
			}
		}

		want := map[string]bool{}
		for _, id := range nodes {
			if merged[id] {
				continue
			}
			ok := true
			for _, d := range deps[id] {
				if !merged[d] {
					ok = false
					break
				}
			}
			if ok {
				want[id] = true
			}
		}

		got, err := db.ListSpawnable(context.Background())
		if err != nil {
			rt.Fatalf("ListSpawnable: %v", err)
		}
		gotSet := map[string]bool{}
		for _, w := range got {
			gotSet[w.ID] = true
		}
		if !setsEqual(want, gotSet) {
			rt.Fatalf("deps=%v merged=%v want=%v got=%v",
				deps, keysSorted(merged), keysSorted(want), keysSorted(gotSet))
		}
	})
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func keysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Use a local insertion-sort to avoid colliding with sort.Strings
	// already imported by work_items_query_test.go's idsOf helper —
	// keeps this file standalone for future reorgs.
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

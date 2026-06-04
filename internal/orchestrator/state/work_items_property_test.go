package state_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestListSpawnable_PropertyTopologicalReady asserts ListSpawnable equals the topological-ready subset over random DAGs of n≤8 nodes (spec §6 Grade-A DoD).
func TestListSpawnable_PropertyTopologicalReady(t *testing.T) {
	tmp := t.TempDir()
	var checkID atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		// Edges flow only from higher to lower index, so the graph is
		// acyclic by construction. Node 0 has no predecessors.
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
		now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
		db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
		if err != nil {
			rt.Fatalf("OpenWithClock: %v", err)
		}
		defer func() { _ = db.Close() }()

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
	sort.Strings(out)
	return out
}

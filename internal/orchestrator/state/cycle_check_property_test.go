package state_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestCycleCheck_PropertyRandomDAG asserts CycleCheck never falsely
// flags a strictly-acyclic graph: edges flow from higher-index to
// lower-index, so no cycle can exist by construction. For every
// node we treat as candidate, CycleCheck must succeed.
func TestCycleCheck_PropertyRandomDAG(t *testing.T) {
	tmp := t.TempDir()
	var checkID atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		deps := make(map[string][]string, n)
		for i := 0; i < n; i++ {
			possible := nodes[:i]
			if len(possible) == 0 {
				deps[nodes[i]] = nil
				continue
			}
			pick := rapid.SliceOfN(rapid.SampledFrom(possible), 0, len(possible)).Draw(rt, "deps_"+nodes[i])
			deps[nodes[i]] = dedupeDeps(pick)
		}

		dbPath := filepath.Join(tmp, fmt.Sprintf("acyc-%d.db", checkID.Add(1)))
		now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
		db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
		if err != nil {
			rt.Fatalf("OpenWithClock: %v", err)
		}
		defer func() { _ = db.Close() }()

		for _, id := range nodes {
			w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id,
				Lane: "server", Status: state.WorkStatusPlanned, DependsOnFeatures: deps[id]}
			if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, now); err != nil {
				rt.Fatalf("upsert %s: %v", id, err)
			}
		}

		for _, id := range nodes {
			cand := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id,
				Lane: "server", Status: state.WorkStatusPlanned, DependsOnFeatures: deps[id]}
			if err := db.CycleCheck(context.Background(), cand); err != nil {
				rt.Fatalf("CycleCheck flagged acyclic node %s as cyclic: deps=%v err=%v",
					id, deps, err)
			}
		}
	})
}

// TestCycleCheck_PropertyPlantedCycle asserts CycleCheck detects any
// directly-planted back-edge. We build the acyclic DAG as above, then
// pick two nodes i<j and ADD an edge from i→j (i.e. i depends on j).
// Since j already transitively reaches i (or could), the augmented
// graph contains a cycle through {i, j}. CycleCheck on i with the
// extra dep must return ErrCycleDetected.
func TestCycleCheck_PropertyPlantedCycle(t *testing.T) {
	tmp := t.TempDir()
	var checkID atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(2, 8).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		// Linear chain — every node depends on its lower-index
		// predecessor. Guarantees the planted back-edge below
		// closes a real cycle.
		deps := make(map[string][]string, n)
		for i := 0; i < n; i++ {
			if i == 0 {
				deps[nodes[i]] = nil
				continue
			}
			deps[nodes[i]] = []string{nodes[i-1]}
		}

		dbPath := filepath.Join(tmp, fmt.Sprintf("cyc-%d.db", checkID.Add(1)))
		now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
		db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
		if err != nil {
			rt.Fatalf("OpenWithClock: %v", err)
		}
		defer func() { _ = db.Close() }()

		for _, id := range nodes {
			w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id,
				Lane: "server", Status: state.WorkStatusPlanned, DependsOnFeatures: deps[id]}
			if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, now); err != nil {
				rt.Fatalf("upsert %s: %v", id, err)
			}
		}

		// Pick the planted-cycle endpoint: any node that's not the
		// chain head can have its predecessor closed into a cycle
		// by adding a forward dep.
		botIdx := rapid.IntRange(1, n-1).Draw(rt, "bot")
		// candidate is node[0]; we make it depend on nodes[botIdx],
		// which already (transitively) depends on nodes[0]. That
		// closes the cycle.
		cand := state.WorkItem{ID: nodes[0], Kind: state.KindFeature, Title: nodes[0],
			Lane: "server", Status: state.WorkStatusPlanned,
			DependsOnFeatures: []string{nodes[botIdx]}}
		err = db.CycleCheck(context.Background(), cand)
		if !errors.Is(err, state.ErrCycleDetected) {
			rt.Fatalf("planted cycle %s -> %s undetected: err=%v",
				nodes[0], nodes[botIdx], err)
		}
	})
}

func dedupeDeps(in []string) []string {
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

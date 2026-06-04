package cycle_test

import (
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/cycle"
)

// TestCheck_EmptyGraph asserts nil error on empty adjacency (#88).
func TestCheck_EmptyGraph(t *testing.T) {
	if err := cycle.Check(map[string][]string{}, "F-A"); err != nil {
		t.Fatalf("empty graph: %v", err)
	}
}

// TestCheck_CandidateUnknownNoEdges asserts unknown candidate w/ no deps passes (#88).
func TestCheck_CandidateUnknownNoEdges(t *testing.T) {
	adj := map[string][]string{"F-A": nil, "F-B": {"F-A"}}
	if err := cycle.Check(adj, "F-C"); err != nil {
		t.Fatalf("unknown candidate: %v", err)
	}
}

// TestCheck_SelfLoop asserts node depending on itself returns ErrCycleDetected (#88).
func TestCheck_SelfLoop(t *testing.T) {
	adj := map[string][]string{"F-A": {"F-A"}}
	err := cycle.Check(adj, "F-A")
	if !errors.Is(err, cycle.ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
}

// TestCheck_TwoNodeCycle asserts A->B B->A returns ErrCycleDetected (#88).
func TestCheck_TwoNodeCycle(t *testing.T) {
	adj := map[string][]string{"F-A": {"F-B"}, "F-B": {"F-A"}}
	err := cycle.Check(adj, "F-A")
	if !errors.Is(err, cycle.ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
}

// TestCheck_ThreeNodeCycle asserts A->B->C->A returns ErrCycleDetected (#88).
func TestCheck_ThreeNodeCycle(t *testing.T) {
	adj := map[string][]string{
		"F-A": {"F-B"},
		"F-B": {"F-C"},
		"F-C": {"F-A"},
	}
	err := cycle.Check(adj, "F-A")
	if !errors.Is(err, cycle.ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
}

// TestCheck_DeepLinearChain asserts a 50-node linear DAG passes (#88).
func TestCheck_DeepLinearChain(t *testing.T) {
	adj := map[string][]string{}
	for i := 0; i < 50; i++ {
		id := "F-" + string(rune('A'+i%26)) + string(rune('A'+i/26))
		if i == 0 {
			adj[id] = nil
		} else {
			prev := "F-" + string(rune('A'+(i-1)%26)) + string(rune('A'+(i-1)/26))
			adj[id] = []string{prev}
		}
	}
	if err := cycle.Check(adj, "F-AA"); err != nil {
		t.Fatalf("deep chain rejected: %v", err)
	}
}

// TestCheck_DiamondAcyclic asserts diamond A->B,C; B,C->D passes (#88).
func TestCheck_DiamondAcyclic(t *testing.T) {
	adj := map[string][]string{
		"F-A": nil,
		"F-B": {"F-A"},
		"F-C": {"F-A"},
		"F-D": {"F-B", "F-C"},
	}
	if err := cycle.Check(adj, "F-D"); err != nil {
		t.Fatalf("diamond rejected: %v", err)
	}
}

// TestCheck_UnknownDepEdgeIgnored asserts dangling refs do not trip detection (#88).
func TestCheck_UnknownDepEdgeIgnored(t *testing.T) {
	adj := map[string][]string{
		"F-A": {"F-MISSING"},
	}
	if err := cycle.Check(adj, "F-A"); err != nil {
		t.Fatalf("dangling dep treated as cycle: %v", err)
	}
}

// TestCheck_PropertyRandomDAG asserts random DAGs never trip ErrCycleDetected (#88).
func TestCheck_PropertyRandomDAG(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 12).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		adj := make(map[string][]string, n)
		for i := 0; i < n; i++ {
			possible := nodes[:i]
			if len(possible) == 0 {
				adj[nodes[i]] = nil
				continue
			}
			pick := rapid.SliceOfN(rapid.SampledFrom(possible), 0, len(possible)).Draw(rt, "deps_"+nodes[i])
			adj[nodes[i]] = dedupe(pick)
		}
		for _, id := range nodes {
			if err := cycle.Check(adj, id); err != nil {
				rt.Fatalf("acyclic flagged: id=%s adj=%v err=%v", id, adj, err)
			}
		}
	})
}

// TestCheck_PropertyPlantedCycle asserts back-edge into linear chain trips ErrCycleDetected (#88).
func TestCheck_PropertyPlantedCycle(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(2, 12).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		adj := make(map[string][]string, n)
		for i := 0; i < n; i++ {
			if i == 0 {
				adj[nodes[i]] = nil
				continue
			}
			adj[nodes[i]] = []string{nodes[i-1]}
		}
		// Close cycle: make nodes[0] depend on nodes[botIdx]
		botIdx := rapid.IntRange(1, n-1).Draw(rt, "bot")
		adj[nodes[0]] = []string{nodes[botIdx]}
		err := cycle.Check(adj, nodes[0])
		if !errors.Is(err, cycle.ErrCycleDetected) {
			rt.Fatalf("planted cycle %s -> %s undetected: adj=%v err=%v",
				nodes[0], nodes[botIdx], adj, err)
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

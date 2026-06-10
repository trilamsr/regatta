// Package transitions tests — pure data tables, no DB.
package transitions

import (
	"testing"

	"pgregory.net/rapid"
)

// TestAgentEdges_TerminalsHaveNoOutgoing pins terminal-set invariant for the agent FSM (#795 T3).
func TestAgentEdges_TerminalsHaveNoOutgoing(t *testing.T) {
	cases := []struct {
		state string
		want  int
	}{
		{"done", 0},
		{"withdrawn", 0},
		{"escalated", 0},
		{"crashed", 2}, // crashed→pending recovery edge; crashed→withdrawn cascade-archive edge
	}
	for _, c := range cases {
		got := len(AgentEdges[c.state])
		if got != c.want {
			t.Errorf("AgentEdges[%q] has %d outgoing edges; want %d", c.state, got, c.want)
		}
	}
}

// TestAgentEdges_CrashedRequeues pins crashed→pending merge-recovery edge (#795 T3).
func TestAgentEdges_CrashedRequeues(t *testing.T) {
	if _, ok := AgentEdges["crashed"]["pending"]; !ok {
		t.Fatal("AgentEdges[crashed][pending] missing — merge-recovery requeue edge broken")
	}
}

// TestAgentEdges_CrashedCanWithdraw pins crashed→withdrawn cascade-archive edge.
// When TombstoneBySource archives a work_item, any agent bound to it
// and still in crashed (recovered but not yet requeued to pending)
// MUST be cascadable to withdrawn — otherwise the agent re-spawns
// after the next Recover→Tick cycle (ghost-recovery storm #1208 retro).
func TestAgentEdges_CrashedCanWithdraw(t *testing.T) {
	if _, ok := AgentEdges["crashed"]["withdrawn"]; !ok {
		t.Fatal("AgentEdges[crashed][withdrawn] missing — tombstone cascade cannot drop crashed orphans")
	}
}

// TestAgentEdges_PendingSelfEdge pins pending→pending spec-watcher idempotent edge (#795 T3).
func TestAgentEdges_PendingSelfEdge(t *testing.T) {
	if _, ok := AgentEdges["pending"]["pending"]; !ok {
		t.Fatal("AgentEdges[pending][pending] missing — spec watcher idempotent upsert broken")
	}
}

// TestAgentEdges_AwaitingMergeCanCrash pins awaiting_merge→crashed merge-recovery edge (#795 T3).
func TestAgentEdges_AwaitingMergeCanCrash(t *testing.T) {
	if _, ok := AgentEdges["awaiting_merge"]["crashed"]; !ok {
		t.Fatal("AgentEdges[awaiting_merge][crashed] missing — merge-recovery edge broken")
	}
}

// TestAgentEdges_RunningCanGoPROpen pins running→pr_open happy-path edge (#795 T3).
func TestAgentEdges_RunningCanGoPROpen(t *testing.T) {
	if _, ok := AgentEdges["running"]["pr_open"]; !ok {
		t.Fatal("AgentEdges[running][pr_open] missing")
	}
}

// TestAgentEdges_NoUnknownStateKey rejects typo'd states not in the 11-value AgentState enum (#795 T3).
func TestAgentEdges_NoUnknownStateKey(t *testing.T) {
	known := map[string]struct{}{
		"pending": {}, "spawning": {}, "running": {}, "pr_open": {},
		"gates_running": {}, "awaiting_merge": {}, "gates_failed": {},
		"done": {}, "withdrawn": {}, "crashed": {}, "escalated": {},
	}
	for src, outgoing := range AgentEdges {
		if _, ok := known[src]; !ok {
			t.Errorf("AgentEdges has unknown source state %q", src)
		}
		for dst := range outgoing {
			if _, ok := known[dst]; !ok {
				t.Errorf("AgentEdges[%q] has unknown dest state %q", src, dst)
			}
		}
	}
}

// TestWorkItemEdges_TerminalsHaveNoOutgoing pins terminal-set invariant for work_items.status (#795 T3).
func TestWorkItemEdges_TerminalsHaveNoOutgoing(t *testing.T) {
	for _, s := range []string{"merged", "archived", "rejected"} {
		if got := len(WorkItemEdges[s]); got != 0 {
			t.Errorf("WorkItemEdges[%q] has %d outgoing edges; want 0", s, got)
		}
	}
}

// TestWorkItemEdges_PlannedCanReject pins planned→rejected approval-gate denial edge (#795 T3).
func TestWorkItemEdges_PlannedCanReject(t *testing.T) {
	if _, ok := WorkItemEdges["planned"]["rejected"]; !ok {
		t.Fatal("WorkItemEdges[planned][rejected] missing — approval-gate denial path broken")
	}
}

// TestWorkItemEdges_NoUnknownStateKey rejects typo'd statuses not in the 7-value WorkItemStatus enum (#795 T3).
func TestWorkItemEdges_NoUnknownStateKey(t *testing.T) {
	known := map[string]struct{}{
		"planned": {}, "running": {}, "pr_open": {}, "merged": {},
		"archived": {}, "blocked": {}, "rejected": {},
	}
	for src, outgoing := range WorkItemEdges {
		if _, ok := known[src]; !ok {
			t.Errorf("WorkItemEdges has unknown source status %q", src)
		}
		for dst := range outgoing {
			if _, ok := known[dst]; !ok {
				t.Errorf("WorkItemEdges[%q] has unknown dest status %q", src, dst)
			}
		}
	}
}

// TestAgentEdges_DestKeysExistAsSources_Property asserts every dest state appears as a source key (#795 T3).
func TestAgentEdges_DestKeysExistAsSources_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		src := rapid.SampledFrom(keys(AgentEdges)).Draw(rt, "src")
		for dst := range AgentEdges[src] {
			if _, ok := AgentEdges[dst]; !ok {
				rt.Errorf("dest state %q (from %q) missing as source key", dst, src)
			}
		}
	})
}

func keys(m map[string]map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

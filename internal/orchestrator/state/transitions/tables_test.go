// Package transitions tests — pure data tables, no DB.
package transitions

import "testing"

// TestAgentEdges_TerminalsHaveNoOutgoing pins terminal-set invariant
// (done, withdrawn, escalated have no outgoing edges; crashed has one
// recovery edge to pending).
func TestAgentEdges_TerminalsHaveNoOutgoing(t *testing.T) {
	cases := []struct {
		state string
		want  int
	}{
		{"done", 0},
		{"withdrawn", 0},
		{"escalated", 0},
		{"crashed", 1}, // crashed→pending recovery edge
	}
	for _, c := range cases {
		got := len(AgentEdges[c.state])
		if got != c.want {
			t.Errorf("AgentEdges[%q] has %d outgoing edges; want %d", c.state, got, c.want)
		}
	}
}

// TestAgentEdges_CrashedRequeues pins the crashed→pending requeue edge
// (load-bearing — merge-recovery uses it per agents.go comment).
func TestAgentEdges_CrashedRequeues(t *testing.T) {
	if _, ok := AgentEdges["crashed"]["pending"]; !ok {
		t.Fatal("AgentEdges[crashed][pending] missing — merge-recovery requeue edge broken")
	}
}

// TestAgentEdges_PendingSelfEdge pins pending→pending (spec watcher
// upsert without bookkeeping).
func TestAgentEdges_PendingSelfEdge(t *testing.T) {
	if _, ok := AgentEdges["pending"]["pending"]; !ok {
		t.Fatal("AgentEdges[pending][pending] missing — spec watcher idempotent upsert broken")
	}
}

// TestAgentEdges_AwaitingMergeCanCrash pins awaiting_merge→crashed
// (merge-recovery requeue path per PHASE AUTONOMY §11 W2 c0).
func TestAgentEdges_AwaitingMergeCanCrash(t *testing.T) {
	if _, ok := AgentEdges["awaiting_merge"]["crashed"]; !ok {
		t.Fatal("AgentEdges[awaiting_merge][crashed] missing — merge-recovery edge broken")
	}
}

// TestAgentEdges_RunningCanGoPROpen pins running→pr_open (the canonical
// happy-path agent edge).
func TestAgentEdges_RunningCanGoPROpen(t *testing.T) {
	if _, ok := AgentEdges["running"]["pr_open"]; !ok {
		t.Fatal("AgentEdges[running][pr_open] missing")
	}
}

// TestAgentEdges_NoUnknownStateKey ensures the table doesn't gain a
// typo'd state via a refactor — every key must be one of the 11 enum
// values in state.go.
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

// TestWorkItemEdges_TerminalsHaveNoOutgoing pins terminal-set invariant
// for work-item edges (merged, archived, rejected are terminal per
// spec §3.1).
func TestWorkItemEdges_TerminalsHaveNoOutgoing(t *testing.T) {
	for _, s := range []string{"merged", "archived", "rejected"} {
		if got := len(WorkItemEdges[s]); got != 0 {
			t.Errorf("WorkItemEdges[%q] has %d outgoing edges; want 0", s, got)
		}
	}
}

// TestWorkItemEdges_PlannedCanReject pins the approval-gate edge
// (scheduler_approval_gate.go: planned→rejected on denial).
func TestWorkItemEdges_PlannedCanReject(t *testing.T) {
	if _, ok := WorkItemEdges["planned"]["rejected"]; !ok {
		t.Fatal("WorkItemEdges[planned][rejected] missing — approval-gate denial path broken")
	}
}

// TestWorkItemEdges_NoUnknownStateKey ensures the table doesn't gain a
// typo'd status via a refactor — every key must be one of the 7 enum
// values in work_items.go.
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

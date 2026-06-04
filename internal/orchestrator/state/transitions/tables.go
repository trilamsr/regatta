// Package transitions is the pure-data agent + work-item edge table
// consumed by state.TransitionAgentTx. One-direction: MUST NOT import
// state (docs/engineer/specs/2026-06-04-state-package-split-design.md §4.2).
package transitions

// AgentEdges is the agent state-machine adjacency map (docs/design.md §378).
// Keys are string forms of state.AgentState; crashed→pending is the
// merge-recovery requeue edge (PHASE AUTONOMY §11 W2 c0).
var AgentEdges = map[string]map[string]struct{}{
	"pending": {
		"pending":   {},
		"spawning":  {},
		"withdrawn": {},
	},
	"spawning": {
		"running": {},
		"crashed": {},
	},
	"running": {
		"pr_open": {},
		"crashed": {},
	},
	"pr_open": {
		"gates_running": {},
		"withdrawn":     {},
		"crashed":       {},
	},
	"gates_running": {
		"awaiting_merge": {},
		"gates_failed":   {},
		"crashed":        {},
	},
	"gates_failed": {
		"running":   {},
		"escalated": {},
		"withdrawn": {},
	},
	"awaiting_merge": {
		"done":      {},
		"withdrawn": {},
		"crashed":   {},
	},
	"done":      {},
	"withdrawn": {},
	"crashed":   {"pending": {}},
	"escalated": {},
}

// WorkItemEdges documents the work_items.status edges exercised by the
// scheduler (spec §3.1). SQL CAS in TransitionWorkItem is the hard
// enforcement layer; this table is advisory + future-enforcement-ready.
var WorkItemEdges = map[string]map[string]struct{}{
	"planned": {
		"running":  {},
		"rejected": {},
		"archived": {},
		"blocked":  {},
	},
	"running": {
		"pr_open":  {},
		"blocked":  {},
		"archived": {},
	},
	"pr_open": {
		"merged":   {},
		"archived": {},
	},
	"blocked": {
		"planned":  {},
		"archived": {},
	},
	"merged":   {},
	"archived": {},
	"rejected": {},
}

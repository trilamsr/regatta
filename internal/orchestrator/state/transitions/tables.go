// Package transitions is the pure-data agent + work-item edge tables consumed by state (one-direction: never imports state); see specs/2026-06-04-state-package-split-design.md §4.2.
package transitions

// AgentEdges is the agent state-machine adjacency map (docs/design.md §378); crashed→pending is the merge-recovery requeue edge.
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

// WorkItemEdges documents scheduler-exercised work_items.status edges (spec §3.1); SQL CAS in TransitionWorkItem stays the hard enforcement, this table is advisory.
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

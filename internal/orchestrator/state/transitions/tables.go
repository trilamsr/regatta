// Package transitions defines the agent + work-item state-machine
// edges as pure data. It MUST NOT import state — one-direction rule
// per docs/engineer/specs/2026-06-04-state-package-split-design.md
// §4.2. The state package consumes these maps via string-keyed
// lookups; see state.TransitionAgentTx.
package transitions

// AgentEdges encodes the valid agent state transitions from
// docs/design.md §378. Keys are string forms of state.AgentState.
// Self-edges + idempotent re-applies are allowed where useful (e.g.
// pending→pending lets the spec watcher upsert without bookkeeping).
// AgentCrashed has a single outgoing edge to AgentPending so
// merge-recovery can requeue agents whose merge intent never
// materialized (PHASE AUTONOMY §11 W2 c0).
var AgentEdges = map[string]map[string]struct{}{}

// WorkItemEdges encodes the work_items.status transitions exercised
// by the scheduler (spec §3.1). SQL CAS in TransitionWorkItem is the
// hard enforcement layer; this table documents the intended edge set
// for code review, future enforcement, and cross-package validation.
// Terminal states (merged, archived, rejected) have no outgoing edges.
var WorkItemEdges = map[string]map[string]struct{}{}

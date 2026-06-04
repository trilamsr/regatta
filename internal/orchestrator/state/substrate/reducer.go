package substrate

// ReducerStrategy names the fold semantics per kind. Wave 1 hardcodes
// in defaultReducer(); policy-driven override deferred to W11
// blackboard (followup F10).
type ReducerStrategy string

const (
	// StrategyLWW — most-recent by (written_at DESC, id DESC). Suits
	// node_output, fact, budget_reconciled, heartbeat.
	StrategyLWW ReducerStrategy = "lww"

	// StrategyAppend — every event a distinct row. Suits approval_event,
	// token_spend (SUM over window), gate_verdict (signed chain).
	StrategyAppend ReducerStrategy = "append"

	// StrategyWriteOnce — only the first event per (run, kind, key) is
	// authoritative. Reserved; no Wave 1 kind uses it.
	StrategyWriteOnce ReducerStrategy = "write-once"
)

// defaultReducer returns the spec §4 strategy for kind. Hardcoded per
// spec — deviation requires re-spawning the design subagent
// (feedback_spec_pattern_authority). Default arm fails-closed to
// append; SQL CHECK on substrate_events.kind blocks unknown kinds at
// INSERT, so the default should be unreachable.
func defaultReducer(kind EventKind) ReducerStrategy {
	switch kind {
	case KindNodeOutput, KindFact, KindBudgetReconciled, KindHeartbeat:
		return StrategyLWW
	case KindApprovalEvent, KindTokenSpend, KindGateVerdict,
		KindBriefRejected, KindPRStageTransition,
		KindManualMerge, KindOperatorIntervention,
		KindCostCapThrottled, KindCostCapResumed:
		return StrategyAppend
	default:
		return StrategyAppend
	}
}

// DefaultReducer exposes the baseline strategy for tests and the W11
// policy table that wants to read it before override.
func DefaultReducer(kind EventKind) ReducerStrategy {
	return defaultReducer(kind)
}

package substrate

// ReducerStrategy names the fold semantics per kind. Wave 1 hardcodes
// the strategy per kind in defaultReducer(); policy-driven override
// is deferred to W11 blackboard (followup F10).
type ReducerStrategy string

const (
	// StrategyLWW selects the most-recent event by (written_at DESC,
	// id DESC). Suits node_output (latest attempt), fact (latest write
	// per key), budget_reconciled (latest correction), heartbeat
	// (latest liveness).
	StrategyLWW ReducerStrategy = "lww"

	// StrategyAppend keeps every event as a distinct row; fold returns
	// all. Suits approval_event (state-machine transitions),
	// token_spend (budget = SUM over window), gate_verdict (signed
	// verdict chain).
	StrategyAppend ReducerStrategy = "append"

	// StrategyWriteOnce treats only the first event per (run, kind,
	// key) as authoritative; later events ⇒ ErrReplay-equivalent
	// rejection at the fold layer. Reserved; no Wave 1 kind uses it
	// but reducers authored in W11 may opt in.
	StrategyWriteOnce ReducerStrategy = "write-once"
)

// defaultReducer returns the spec §4 reducer strategy for kind.
// Hardcoded per spec — implementer deviation requires re-spawning the
// design subagent (feedback_spec_pattern_authority).
//
//   - node_output, fact, budget_reconciled, heartbeat → lww
//   - approval_event, token_spend, gate_verdict       → append
func defaultReducer(kind EventKind) ReducerStrategy {
	switch kind {
	case KindNodeOutput, KindFact, KindBudgetReconciled, KindHeartbeat:
		return StrategyLWW
	case KindApprovalEvent, KindTokenSpend, KindGateVerdict:
		return StrategyAppend
	default:
		// Unknown kind: fail-closed to append (no data lost; reducer
		// override never silently drops). Callers should not see this
		// path — the SQL CHECK on substrate_events.kind blocks unknown
		// kinds at INSERT time.
		return StrategyAppend
	}
}

// DefaultReducer is the exported view of defaultReducer for tests and
// for the eventual W11 policy table that wants to know the baseline
// before override.
func DefaultReducer(kind EventKind) ReducerStrategy {
	return defaultReducer(kind)
}

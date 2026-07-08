package gate

// WorkItemScope holds the read-only scope keys for one Gate.Evaluate
// call (spec §3.2 Request shape). The scheduler builds from
// state.WorkItem; the spawner SupervisorLimits builds from the
// running agent's known model + cumulative spend. AllowDowngrade is
// the resolved `work_item.annotations.cost.allow_downgrade` (spec §3.6
// + R10 mitigation) — default false keeps WARN-only posture intact.
type WorkItemScope struct {
	WorkItemID     string
	DAGID          string
	OperatorID     string  // agent_id; same shape as obs.KeyAgentID
	TenantID       string  // substrate.DefaultTenantID until W8
	Model          string  // request-target model
	AllowDowngrade bool    // opt-in soft-cap downgrade gate (R10)
	EstHint        EstHint // zero-valued hint is rejected by UpperBound.Estimate as ErrEmptyHint
}

// EstHint is the planner-side cost hint. USD set → use as upper-bound
// (planner resolved); USD==0 with InputTokens+MaxTokens populated →
// ask estimator to compute. OperatorID is unused by default
// upper_bound; populated by Gate.Evaluate when the History estimator
// (#238 opt-in) needs cohort scoping at p95-query time.
type EstHint struct {
	USD         float64
	InputTokens int64
	MaxTokens   int64
	OperatorID  string
}

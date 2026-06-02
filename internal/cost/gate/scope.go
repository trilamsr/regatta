package gate

// WorkItemScope holds the read-only scope keys for one Gate.Evaluate
// call. Spec §3.2 lines 168-175 (Request shape). The scheduler builds
// this from state.WorkItem; the spawner SupervisorLimits builds this
// from the running agent's currently-known model + cumulative spend.
//
// AllowDowngrade is the resolved value of the
// `work_item.annotations.cost.allow_downgrade` annotation (spec §3.6
// + R10 mitigation). Scheduler-side resolver populates it; default
// false keeps the WARN-only posture intact.
type WorkItemScope struct {
	WorkItemID     string
	DAGID          string
	OperatorID     string  // agent_id; same shape used in obs.KeyAgentID
	TenantID       string  // substrate.DefaultTenantID until W8
	Model          string  // request-target model (default per regatta.yaml)
	AllowDowngrade bool    // opt-in soft-cap downgrade gate (R10)
	EstHint        EstHint // optional planner hint; zero-value falls back to upper-bound default
}

// EstHint is the planner-side cost hint surface. Implementers from
// T2 (internal/cost/estimate) own the production estimator; the hint
// here is a tiny seam so the planner can pass a per-call upper-bound
// without forcing every caller through the estimator package.
//
// USD set means "use this as the upper-bound" (planner already
// resolved); USD==0 with InputTokens+MaxTokens populated means "ask
// the estimator to compute from these counts".
//
// OperatorID is the per-call agent identity — empty for the default
// upper_bound estimator (which never reads it), populated by Gate.Evaluate
// from WorkItemScope.OperatorID when the History estimator (spec §10 S1
// opt-in, issue #238) needs cohort scoping at p95-query time.
type EstHint struct {
	USD         float64
	InputTokens int64
	MaxTokens   int64
	OperatorID  string
}

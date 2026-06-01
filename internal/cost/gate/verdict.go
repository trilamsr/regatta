package gate

// Verdict is the pre-call decision returned by Gate.Evaluate. Spec
// §3.2 lines 148-156. Field order pinned for diff stability.
type Verdict struct {
	// Allow=true when no cap would be crossed by adding the current
	// upper-bound estimate to recorded spend at every configured scope.
	Allow bool

	// Reason carries a structured deny-cause when Allow=false. Shape:
	// "cap_exceeded:<scope>:<scope-id>" e.g. "cap_exceeded:dag:DAG-A".
	// Empty when Allow=true.
	Reason string

	// USDEstimate is the upper-bound estimate that drove the decision.
	// Emitted to the cost.evaluate span and mirrored on the llm_call
	// span (T3) so dashboards correlate estimate vs actual.
	USDEstimate float64

	// SoftCapBreached signals soft_pct crossed at any configured scope.
	// Acts as a WARN by default; downgrade only fires with the per-wi
	// allow_downgrade annotation (spec R10).
	SoftCapBreached bool

	// DowngradeTo names a cheaper model to swap in when SoftCapBreached
	// AND the wi opted in via allow_downgrade. Empty otherwise.
	DowngradeTo string

	// CapDAGUSD mirrors the active per-DAG cap onto the span (0 sentinel
	// = no cap configured).
	CapDAGUSD float64

	// CapOperatorUSD mirrors the active per-operator cap onto the span.
	CapOperatorUSD float64
}

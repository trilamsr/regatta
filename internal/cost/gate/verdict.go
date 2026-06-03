package gate

// Verdict is the pre-call decision (spec §3.2 lines 148-156). Field
// order pinned for diff stability.
type Verdict struct {
	// Allow=true when no cap would be crossed by adding upper-bound
	// estimate to recorded spend at every configured scope.
	Allow bool

	// Reason carries a structured deny-cause when Allow=false:
	// "cap_exceeded:<scope>:<scope-id>" (e.g. dag:DAG-A).
	Reason string

	// USDEstimate is the upper-bound that drove the decision. Mirrored
	// onto the llm_call span so dashboards correlate estimate vs actual.
	USDEstimate float64

	// SoftCapBreached signals soft_pct crossed at any scope. WARN by
	// default; downgrade only fires with the per-wi allow_downgrade
	// annotation (spec R10).
	SoftCapBreached bool

	// DowngradeTo names a cheaper model when SoftCapBreached AND the wi
	// opted in via allow_downgrade. Empty otherwise.
	DowngradeTo string

	// CapDAGUSD mirrors the active per-DAG cap onto the span; 0 sentinel
	// means no cap configured.
	CapDAGUSD float64

	// CapOperatorUSD mirrors the active per-operator cap.
	CapOperatorUSD float64
}

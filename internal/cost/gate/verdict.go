package gate

// Verdict is the pre-call decision (spec §3.2 lines 148-156). Field
// order pinned for diff stability. Reason format on deny:
// "cap_exceeded:<scope>:<scope-id>" (e.g. dag:DAG-A). USDEstimate is
// mirrored onto the llm_call span so dashboards correlate estimate
// vs actual. SoftCapBreached fires WARN by default; DowngradeTo
// populates only when per-wi allow_downgrade opted in (spec R10).
// CapDAGUSD / CapOperatorUSD mirror the active caps; 0 = no cap.
type Verdict struct {
	Allow           bool
	Reason          string
	USDEstimate     float64
	SoftCapBreached bool
	DowngradeTo     string
	CapDAGUSD       float64
	CapOperatorUSD  float64
}

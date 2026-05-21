package schemas

import "time"

// GateResult is the structured payload every gate emits. Matches
// schemas/gate_result.schema.json.
type GateResult struct {
	GateID     string         `json:"gate_id"`
	GateKind   string         `json:"gate_kind"` // "deterministic" | "ai_judicial" | "ai_adversarial" | "ai_rule"
	Verdict    Verdict        `json:"verdict"`
	Findings   []Finding      `json:"findings"`
	Telemetry  Telemetry      `json:"telemetry"`
	PRSha      string         `json:"pr_sha"`
	RunID      string         `json:"run_id"`
	Signature  string         `json:"signature,omitempty"` // HMAC over the rest
}

// Verdict is the gate's overall outcome.
type Verdict string

// Verdict values; the only legal payload of GateResult.Verdict.
const (
	VerdictPass Verdict = "pass"
	VerdictFail Verdict = "fail"
	VerdictSkip Verdict = "skip"
)

// Finding is a single rejecting (or informational) signal a gate emits.
type Finding struct {
	Severity    Severity `json:"severity"`
	Message     string   `json:"message"`
	Path        string   `json:"path,omitempty"`
	Line        int      `json:"line,omitempty"`
	TrapPattern string   `json:"trap_pattern,omitempty"` // "P1" .. "P13", or empty
	Blocking    bool     `json:"blocking"`
}

// Severity orders Finding gravity, ascending.
type Severity string

// Severity values; the only legal payload of Finding.Severity.
const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Telemetry captures gate timing + cost. Fed into observability paths.
type Telemetry struct {
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	CostUSD    float64   `json:"cost_usd,omitempty"`
	ModelID    string    `json:"model_id,omitempty"`
}

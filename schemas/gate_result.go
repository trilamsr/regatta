package schemas

import "time"

// GateResult is the structured payload every gate emits. Matches
// schemas/gate_result.schema.json field-for-field.
//
// Schema-lockstep discipline: any field added here must also be
// added to gate_result.schema.json (and vice versa). The
// TestGateResultSchemaLockstep test in gate_result_test.go round-
// trips a fixture through both shapes and fails on drift.
type GateResult struct {
	SchemaVersion     int            `json:"schema_version"`
	GateID            string         `json:"gate_id"`
	GateKind          GateKind       `json:"gate_kind"`
	PRSHA             string         `json:"pr_sha"`
	BaseSHA           string         `json:"base_sha,omitempty"`
	RunID             string         `json:"run_id"`
	Verdict           Verdict        `json:"verdict"`
	Blocking          bool           `json:"blocking"`
	Severity          Severity       `json:"severity,omitempty"`
	Findings          []Finding      `json:"findings"`
	InjectionSuspected bool          `json:"injection_suspected,omitempty"`
	Telemetry         Telemetry      `json:"telemetry"`
	Signature         SignatureBlock `json:"signature"`
}

// GateKind matches gate_result.schema.json enum.
type GateKind string

const (
	GateKindDeterministic   GateKind = "deterministic"
	GateKindAIJudicial      GateKind = "ai_judicial"
	GateKindAIAdversarial   GateKind = "ai_adversarial"
	GateKindAIRuleCheck     GateKind = "ai_rule_check"
)

// Verdict matches the schema enum. There is no "skip" verdict;
// gates that do not apply emit a "pass" with empty findings and
// telemetry.skipped_reason set, or are not invoked at all.
type Verdict string

const (
	VerdictPass     Verdict = "pass"
	VerdictFail     Verdict = "fail"
	VerdictAdvisory Verdict = "advisory"
)

// Severity matches gate_result.schema.json severity enum on the
// top-level GateResult (note: findings[].severity uses a separate
// enum that includes "info"; see FindingSeverity).
type Severity string

const (
	SeverityNone     Severity = "none"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// FindingSeverity is the per-finding severity enum. Schema allows
// "info" here but not at the top level.
type FindingSeverity string

const (
	FindingInfo     FindingSeverity = "info"
	FindingLow      FindingSeverity = "low"
	FindingMedium   FindingSeverity = "medium"
	FindingHigh     FindingSeverity = "high"
	FindingCritical FindingSeverity = "critical"
)

// Finding matches schemas/gate_result.schema.json findings[]. The
// Go struct used to flatten path+line into top-level fields; the
// schema requires them nested under evidence. The Go struct is now
// the schema shape.
type Finding struct {
	ID          string           `json:"id"`
	Severity    FindingSeverity  `json:"severity"`
	Claim       string           `json:"claim"`
	Evidence    *FindingEvidence `json:"evidence,omitempty"`
	Remediation string           `json:"remediation,omitempty"`
	TrapPattern string           `json:"trap_pattern,omitempty"` // "P1" .. "P13"
}

type FindingEvidence struct {
	Path      string `json:"path,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	SHA       string `json:"sha,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
}

// Telemetry matches schemas/gate_result.schema.json telemetry.
// Field names previously drifted (started_at vs no started_at,
// model_id vs model). This shape is now the schema shape.
type Telemetry struct {
	DurationMs    int64   `json:"duration_ms"`
	TokensInput   int64   `json:"tokens_input,omitempty"`
	TokensOutput  int64   `json:"tokens_output,omitempty"`
	TokensCached  int64   `json:"tokens_cached,omitempty"`
	CostUSD       float64 `json:"cost_usd,omitempty"`
	Model         string  `json:"model,omitempty"`
	ModelVersion  string  `json:"model_version,omitempty"`
	PromptSHA     string  `json:"prompt_sha,omitempty"`

	// StartedAt and FinishedAt are heartbeat anchors -- not part of
	// the published schema, but the audit reconciler needs them for
	// silent-bypass detection. Carried in-process; stripped before
	// signing.
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

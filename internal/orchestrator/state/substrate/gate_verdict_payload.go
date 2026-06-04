package substrate

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// GateVerdictPayload is the typed shape of a kind=gate_verdict event's
// payload; the substrate validator rejects malformed shapes pre-HMAC
// so a hostile producer cannot smuggle past the cycle/replay checks.
//
// Issue #550 reframe: non-determinism sources (LLM model id, scanner
// tool + DB snapshot, schema version at decision time) are journaled
// into the payload so an auditor can name which dependency produced
// the result even when the verdict is not bit-replayable. JSON keys
// are short to stay under the 1024-byte CHECK on substrate_events.
type GateVerdictPayload struct {
	GateName   string `json:"gate_name"`
	Pass       bool   `json:"pass"`
	Reason     string `json:"reason"`
	WorkItemID string `json:"work_item_id"`

	// Tool names the verdict producer ("cel", "gitleaks", "opa",
	// "anthropic-api", ...). Required; audit-verify groups by tool.
	Tool string `json:"tool"`

	// ModelOrVersion pins the producer version (git-SHA, tool version,
	// model id + snapshot, vuln-DB snapshot id). Required; empty would
	// lie about reproducibility. (tool, version) is the audit anchor.
	ModelOrVersion string `json:"tv"`

	// DBSchemaVersion is the latest applied state.CurrentSchemaVersion
	// at emit time; lets audit-verify flag schema-skew that would shift
	// fold semantics even when the HMAC chain is intact.
	DBSchemaVersion int64 `json:"db_v"`

	// Deterministic is true iff re-running the producer with the same
	// inputs would re-yield Pass/Reason. False ⇒ "verify-only" posture
	// (chain tamper-evident, verdict non-replayable); true ⇒ "reproduce"
	// (chain + re-run must agree).
	Deterministic bool `json:"det"`
}

// NewGateVerdictPayload constructs a payload, rejecting empty metadata
// so the "recorded the tool but not the version" failure mode is
// impossible by construction. Mirrors validateGateVerdict so a payload
// that constructs cleanly also validates cleanly on AppendEvent.
func NewGateVerdictPayload(gateName, workItemID, tool, modelOrVersion string, dbSchemaVersion int64, deterministic bool, pass bool, reason string) (GateVerdictPayload, error) {
	if gateName == "" {
		return GateVerdictPayload{}, fmt.Errorf("%w: gate_verdict missing gate_name", ErrInvalidPayload)
	}
	if workItemID == "" {
		return GateVerdictPayload{}, fmt.Errorf("%w: gate_verdict missing work_item_id", ErrInvalidPayload)
	}
	if tool == "" {
		return GateVerdictPayload{}, fmt.Errorf("%w: gate_verdict missing tool", ErrInvalidPayload)
	}
	if modelOrVersion == "" {
		return GateVerdictPayload{}, fmt.Errorf("%w: gate_verdict missing tool_version", ErrInvalidPayload)
	}
	if dbSchemaVersion <= 0 {
		return GateVerdictPayload{}, fmt.Errorf("%w: gate_verdict db_schema_version must be positive (got %d)", ErrInvalidPayload, dbSchemaVersion)
	}
	return GateVerdictPayload{
		GateName:        gateName,
		Pass:            pass,
		Reason:          reason,
		WorkItemID:      workItemID,
		Tool:            tool,
		ModelOrVersion:  modelOrVersion,
		DBSchemaVersion: dbSchemaVersion,
		Deterministic:   deterministic,
	}, nil
}

// AuditPosture returns "reproduce" or "verify-only" — the operator-
// facing strings rendered by `regatta audit verify` and downstream
// compliance reports. Keep stable; CLI + docs pin against them.
func (p GateVerdictPayload) AuditPosture() string {
	if p.Deterministic {
		return "reproduce"
	}
	return "verify-only"
}

// validateGateVerdict is the dispatch-table entry for KindGateVerdict.
// Strict-unmarshal forbids unknown fields (spec §S4); the (tool, tv,
// db_v) non-empty checks are the issue #550 fix that prevents a
// producer from recording a verdict without naming its dependency.
// Legacy-migration backfill happens at fold time, not here — the
// validator gates new writes only.
func validateGateVerdict(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: gate_verdict payload empty", ErrInvalidPayload)
	}
	var p GateVerdictPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("%w: gate_verdict: %w", ErrInvalidPayload, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: gate_verdict: trailing bytes", ErrInvalidPayload)
	}
	if p.GateName == "" {
		return fmt.Errorf("%w: gate_verdict missing gate_name", ErrInvalidPayload)
	}
	if p.WorkItemID == "" {
		return fmt.Errorf("%w: gate_verdict missing work_item_id", ErrInvalidPayload)
	}
	if p.Tool == "" {
		return fmt.Errorf("%w: gate_verdict missing tool", ErrInvalidPayload)
	}
	if p.ModelOrVersion == "" {
		return fmt.Errorf("%w: gate_verdict missing tool_version", ErrInvalidPayload)
	}
	if p.DBSchemaVersion <= 0 {
		return fmt.Errorf("%w: gate_verdict missing db_schema_version", ErrInvalidPayload)
	}
	return nil
}

func init() {
	RegisterPayloadValidator(KindGateVerdict, validateGateVerdict)
}

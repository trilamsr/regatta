package substrate

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// GateVerdictPayload is the typed shape of a kind=gate_verdict event's
// payload. CELDecider mints these in internal/program/cel_decider.go;
// the substrate validator dispatch rejects malformed shapes before
// HMAC sign so a hostile producer cannot smuggle past the cycle/replay
// checks with garbage bytes.
//
// Issue #550 reframe: non-determinism sources (LLM model id, scanner
// tool + DB snapshot, schema version at decision time) are journaled
// into the payload itself. Re-verification of a recorded verdict
// proves the HMAC chain is intact (tamper-evidence) — it does NOT
// reproduce the verdict bit-for-bit unless Deterministic=true. The
// Tool / ModelOrVersion / DBSchemaVersion fields exist so an auditor
// can name precisely which non-deterministic dependency produced the
// recorded result, even when that result is not replayable.
//
// Fields are stable across schema_version=1; spec §5 forward-version
// migration recipe applies if a future kind change drops or renames
// any. JSON keys are short to stay under the 1024-byte CHECK on
// substrate_events.payload_json.
type GateVerdictPayload struct {
	GateName   string `json:"gate_name"`
	Pass       bool   `json:"pass"`
	Reason     string `json:"reason"`
	WorkItemID string `json:"work_item_id"`

	// Tool names the verdict producer: "cel" (deterministic predicate),
	// "gitleaks", "osv-scanner", "opa", "anthropic-api", etc. Required;
	// the audit-verify CLI groups verdicts by tool so an auditor can
	// see at a glance which gates rely on which external dependency.
	Tool string `json:"tool"`

	// ModelOrVersion pins the producer version that wrote this verdict:
	// a git-SHA (deterministic Go gates), a tool version string
	// ("gitleaks 8.18.4"), a model id + snapshot ("claude-opus-4-7"),
	// or a vuln-DB snapshot id. Required; empty would lie about
	// reproducibility. Compliance teams treat the (tool, version) pair
	// as the audit anchor.
	ModelOrVersion string `json:"tv"`

	// DBSchemaVersion is the latest applied state.CurrentSchemaVersion
	// at verdict-emit time. Lets the audit-verify CLI flag schema-skew
	// (verdict recorded under N, replay under N+M) which would change
	// what fold sees even when the HMAC chain is intact.
	DBSchemaVersion int64 `json:"db_v"`

	// Deterministic flags whether re-running the verdict's producer
	// with the same inputs would re-yield the same Pass/Reason. False
	// for any gate touching an LLM, a CVE database, or a wall-clock
	// scanner; true for CEL predicates and pure-Go gates pinned by
	// git-SHA. Drives the audit posture: false ⇒ "verify-only" (chain
	// is tamper-evident but verdict is non-replayable by construction);
	// true ⇒ "reproduce" (chain + re-run both must agree).
	Deterministic bool `json:"det"`
}

// NewGateVerdictPayload is the constructor that enforces non-empty
// metadata. Callers (CELDecider today; LLM-gate producers when they
// land) pass through here so the "we recorded the tool but not the
// version" failure mode is impossible by construction. Mirrors the
// validator's rejections — a payload that constructs cleanly here
// will also validate cleanly when AppendEvent re-checks the bytes.
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

// AuditPosture returns the audit stance for this verdict's recorded
// metadata. The string is operator-facing: it appears verbatim in the
// `regatta audit verify` output and downstream compliance reports.
//
//   - "reproduce": Deterministic=true — same input must yield same verdict
//     on replay; re-verification can compare bit-for-bit.
//   - "verify-only": Deterministic=false — re-verification proves the
//     HMAC chain is intact (tamper-evidence of the recorded verdict)
//     but the verdict is NOT replayable by construction.
//
// Keep the strings stable; doc and CLI both pin against them.
func (p GateVerdictPayload) AuditPosture() string {
	if p.Deterministic {
		return "reproduce"
	}
	return "verify-only"
}

// validateGateVerdict is the dispatch-table entry registered for
// KindGateVerdict. Strict-unmarshal forbids unknown fields per spec
// §S4; missing required metadata ⇒ ErrInvalidPayload.
// Reason is the operator-facing English string; empty is allowed
// (a passing gate may have nothing to say).
//
// Non-empty validation on (tool, tv, db_v) is the issue #550 fix —
// before this change, a producer could record a verdict without
// naming its tool/model, leaving an auditor unable to tell whether
// the verdict was deterministic. The new fields are REQUIRED on
// every new write; legacy-migration backfill (Tool="unknown-legacy",
// Deterministic=false) is handled at fold time by the audit-verify
// CLI, not at the validator (the validator gates new writes).
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

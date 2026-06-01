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
// Fields are stable across schema_version=1; spec §5 forward-version
// migration recipe applies if a future kind change drops or renames any.
type GateVerdictPayload struct {
	GateName   string `json:"gate_name"`
	Pass       bool   `json:"pass"`
	Reason     string `json:"reason"`
	WorkItemID string `json:"work_item_id"`
}

// validateGateVerdict is the dispatch-table entry registered for
// KindGateVerdict. Strict-unmarshal forbids unknown fields per spec
// §S4; missing gate_name or work_item_id ⇒ ErrInvalidPayload.
// Reason is the operator-facing English string; empty is allowed
// (a passing gate may have nothing to say).
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
	return nil
}

func init() {
	RegisterPayloadValidator(KindGateVerdict, validateGateVerdict)
}

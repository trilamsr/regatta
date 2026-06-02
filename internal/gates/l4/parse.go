package l4

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Envelope is the strict JSON shape adversarial.tmpl §"Output schema"
// asks the model for. Tolerant parsing (fenced-block strip, severity
// normalize, unknown-field tolerate) is applied before Unmarshal so
// the gate stays resilient to model output drift without silently
// dropping critical findings.
type Envelope struct {
	Verdict  string             `json:"verdict"`
	Findings []schemas.Finding  `json:"findings"`
	Notes    string             `json:"notes,omitempty"`
}

// ParseEnvelope decodes one model response into a strict-typed Envelope.
// Tolerance order: strip fenced ```json block, find outermost JSON
// object, Unmarshal, normalize unknown severity tokens to medium.
// Returns error iff no JSON object can be located — refusal-class
// responses surface as L4-INVOKE-ERR via the gate's existing branch.
func ParseEnvelope(raw []byte) (Envelope, error) {
	body := extractJSON(raw)
	if body == "" {
		return Envelope{}, errors.New("l4 parse: no JSON object in model output")
	}
	var env Envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return Envelope{}, fmt.Errorf("l4 parse: %w", err)
	}
	normalizeSeverities(env.Findings)
	return env, nil
}

// extractJSON returns the first balanced top-level JSON object from
// raw, stripping any prose before / after. Handles three shapes:
//   1. Pure JSON ("{...}") — passes through.
//   2. Fenced ```json ... ``` — strips the fence then balances.
//   3. Prose-wrapped — scans for the first '{' and walks brace depth
//      until depth returns to zero.
//
// Strings + escaped chars are tracked so a '}' inside a JSON string
// does not prematurely terminate the scan.
func extractJSON(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "```json"); i >= 0 {
		rest := s[i+len("```json"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		} else {
			s = strings.TrimSpace(rest)
		}
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// normalizeSeverities maps any non-enum severity token onto medium.
// A misbehaving model that emits "BLOCKER" or "showstopper" must not
// silently hide a critical finding from the SeverityBlock router; the
// safe default is to bias toward visibility, not toward pass.
func normalizeSeverities(findings []schemas.Finding) {
	for i := range findings {
		switch findings[i].Severity {
		case schemas.FindingInfo, schemas.FindingLow, schemas.FindingMedium,
			schemas.FindingHigh, schemas.FindingCritical:
			// already valid
		default:
			findings[i].Severity = schemas.FindingMedium
		}
	}
}

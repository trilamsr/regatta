// Package severity is the shared SeverityBlock mini-DSL parser for
// regatta gates. Both internal/gates/l4 (today) and the security +
// approval gates (per spec §3.6 V7) route on the same `["critical",
// "2*high"]` syntax — owning it here keeps the lookup table in one
// place so gate-specific code stays focused on policy, not parsing.
//
// Grammar:
//   - Bare token ("critical", "high"): one matching finding blocks.
//   - "NxSEV" ("2*high"): N or more matching findings block.
package severity

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Critical is the mini-DSL token for "any critical finding blocks."
const Critical = "critical"

// TwoHigh is the mini-DSL token for "2 or more high findings block."
const TwoHigh = "2*high"

// rule is one entry in the parsed SeverityBlock spec.
type rule struct {
	severity schemas.FindingSeverity
	count    int // minimum count needed to trip; >=1
}

func parse(spec []string) ([]rule, error) {
	if len(spec) == 0 {
		return nil, nil
	}
	rules := make([]rule, 0, len(spec))
	for _, raw := range spec {
		tok := strings.TrimSpace(raw)
		count := 1
		sevPart := tok
		if i := strings.IndexByte(tok, '*'); i >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(tok[:i]))
			if err != nil || n < 1 {
				return nil, fmt.Errorf("severity_block: bad count in %q", raw)
			}
			count = n
			sevPart = strings.TrimSpace(tok[i+1:])
		}
		sev := schemas.FindingSeverity(strings.ToLower(sevPart))
		switch sev {
		case schemas.FindingCritical, schemas.FindingHigh, schemas.FindingMedium, schemas.FindingLow, schemas.FindingInfo:
			rules = append(rules, rule{severity: sev, count: count})
		default:
			return nil, fmt.Errorf("severity_block: unknown severity %q in %q", sevPart, raw)
		}
	}
	return rules, nil
}

// Blocks reports whether the SeverityBlock mini-DSL fires against the
// given findings list. Any rule that matches its count threshold
// short-circuits to true. Unparseable rules return false so the gate
// fails-open on bad config (no spurious block on user error). Callers
// MUST run Validate at config-load time so malformed rules surface
// loud at startup.
func Blocks(spec []string, findings []schemas.Finding) bool {
	rules, err := parse(spec)
	if err != nil {
		return false
	}
	counts := make(map[schemas.FindingSeverity]int, len(findings))
	for _, f := range findings {
		counts[f.Severity]++
	}
	for _, r := range rules {
		if counts[r.severity] >= r.count {
			return true
		}
	}
	return false
}

// Validate returns nil iff every SeverityBlock token parses. Callers
// run this at config-load so bad regatta.yaml fails loud at startup
// instead of silently no-op'ing Blocks at Run time.
func Validate(spec []string) error {
	_, err := parse(spec)
	return err
}

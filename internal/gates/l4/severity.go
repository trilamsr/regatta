package l4

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// RuleTwoHigh is the mini-DSL token for "2 or more high findings block."
// Exported so callers + tests share one literal — keeps goconst happy.
const RuleTwoHigh = "2*high"

// RuleCritical is the mini-DSL token for "any critical finding blocks."
const RuleCritical = "critical"

// blockRule is one entry in the SeverityBlock mini-DSL.
//   - Bare token ("critical", "high"): one matching finding blocks.
//   - "NxSEV" ("2*high"): N or more matching findings block.
type blockRule struct {
	severity schemas.FindingSeverity
	count    int // minimum count needed to trip the rule; >=1
}

// parseRules turns SeverityBlock strings into blockRules. The shared
// mini-DSL lives here for the L4 gate alone today; the spec §3.6
// load-bearing extraction into a shared internal/gates/severity
// package is filed as [l4-followup] shared severity parser so the
// security gate can lift this lookup at extraction time.
func parseRules(spec []string) ([]blockRule, error) {
	if len(spec) == 0 {
		return nil, nil
	}
	rules := make([]blockRule, 0, len(spec))
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
			rules = append(rules, blockRule{severity: sev, count: count})
		default:
			return nil, fmt.Errorf("severity_block: unknown severity %q in %q", sevPart, raw)
		}
	}
	return rules, nil
}

// Blocks reports whether the SeverityBlock mini-DSL fires against
// the given findings list. Any rule that matches its count threshold
// short-circuits to true. Unparseable rules return false so the gate
// fails-open on bad config (no spurious block on user error). Callers
// MUST validate the spec via ValidateRules at config-load time so
// malformed rules surface loud at startup.
func Blocks(spec []string, findings []schemas.Finding) bool {
	rules, err := parseRules(spec)
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

// ValidateRules returns nil iff every SeverityBlock token parses.
// Callers run this at config-load so bad regatta.yaml fails loud at
// startup instead of silently no-op'ing Blocks at Run time.
func ValidateRules(spec []string) error {
	_, err := parseRules(spec)
	return err
}

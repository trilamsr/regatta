package approval

import (
	"regexp"
	"strings"
)

// matchedCriterion is a single acceptance-criterion line extracted
// from a markdown checkbox list. Internal-only.
type matchedCriterion struct {
	State    L0CriterionState
	Text     string // text after the checkbox, before any citation trailer
	Citation string // empty if absent
	Line     int    // 1-based line number in the source
}

// CriterionState is planned ("- [ ]") or done ("- [x]"). L0 forbids
// done→planned flips and enforces byte-identical text on done→done.
type L0CriterionState int

// Criterion lifecycle states. iota ordering must match String().
const (
	L0StatePlanned L0CriterionState = iota
	L0StateDone
)

func (s L0CriterionState) String() string {
	if s == L0StateDone {
		return "done"
	}
	return "planned"
}

var (
	checkboxRe = regexp.MustCompile(`^\s*-\s+\[([ xX])\]\s+(.*)$`)
	// Citation trailer: one or more space-separated `(test|file|commit)=value`
	// fragments at end of line. Comma-separated also accepted within a fragment.
	criteriaCitationRe = regexp.MustCompile(`\s+((?:test|file|commit)=\S+(?:,\s*(?:test|file|commit)=\S+)*)\s*$`)
)

// Extract pulls every checkbox criterion from a markdown blob. Order
// is preserved; ID is implicit (position).
func l0Extract(content string) []matchedCriterion {
	var out []matchedCriterion
	for i, line := range strings.Split(content, "\n") {
		m := checkboxRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		state := L0StatePlanned
		if m[1] == "x" || m[1] == "X" {
			state = L0StateDone
		}
		text := m[2]
		citation := ""
		if cm := criteriaCitationRe.FindStringSubmatch(text); cm != nil {
			citation = cm[1]
			text = strings.TrimSpace(text[:len(text)-len(cm[0])])
		}
		out = append(out, matchedCriterion{
			State:    state,
			Text:     text,
			Citation: citation,
			Line:     i + 1,
		})
	}
	return out
}

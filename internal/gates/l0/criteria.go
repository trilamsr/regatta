package l0

import (
	"regexp"
	"strings"
)

// Criterion is a single acceptance-criterion line extracted from a
// markdown checkbox list.
type Criterion struct {
	State    CriterionState
	Text     string // text after the checkbox glyph and before any citation trailer
	Citation string // empty if absent
	Line     int    // 1-based line number in the source
}

// CriterionState is the two-state lifecycle of a markdown criterion:
// planned ("- [ ]") or done ("- [x]"). L0 enforces that no PR may flip
// done→planned and that done→done text is byte-identical post-normalize.
type CriterionState int

// Criterion lifecycle states. iota ordering must match String().
const (
	StatePlanned CriterionState = iota
	StateDone
)

func (s CriterionState) String() string {
	if s == StateDone {
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
func Extract(content string) []Criterion {
	var out []Criterion
	for i, line := range strings.Split(content, "\n") {
		m := checkboxRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		state := StatePlanned
		if m[1] == "x" || m[1] == "X" {
			state = StateDone
		}
		text := m[2]
		citation := ""
		if cm := criteriaCitationRe.FindStringSubmatch(text); cm != nil {
			citation = cm[1]
			text = strings.TrimSpace(text[:len(text)-len(cm[0])])
		}
		out = append(out, Criterion{
			State:    state,
			Text:     text,
			Citation: citation,
			Line:     i + 1,
		})
	}
	return out
}

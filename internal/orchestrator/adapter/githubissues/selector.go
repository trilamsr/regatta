package githubissues

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultSelectorState pins the default GH issue state the adapter polls when cfg.Selector omits the `state:` clause.
const DefaultSelectorState = "open"

type parsedSelector struct {
	labels []string
	state  string
}

func defaultSelector() parsedSelector {
	return parsedSelector{labels: []string{AutonomousLabel}, state: DefaultSelectorState}
}

// apiLabel renders the AND-set as the comma-joined value `gh issue list --label` AND-combines server-side (#1076).
func (s parsedSelector) apiLabel() string {
	return strings.Join(s.labels, ",")
}

// parseSelector accepts repeated `label:<name>` clauses (AND-combined, mapped to GH's comma-separated labels filter) plus one `state:<value>`, space-separated. Reject exact-duplicate labels, duplicate state, and whitespace-only values so a typo cannot silently fall back to the default. Closes #1067; multi-label AND closes #1076.
func parseSelector(raw string) (parsedSelector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSelector(), nil
	}
	out := parsedSelector{state: DefaultSelectorState}
	seenLabel := map[string]struct{}{}
	gotState := false
	for _, tok := range strings.Fields(raw) {
		colon := strings.IndexByte(tok, ':')
		if colon <= 0 || colon == len(tok)-1 {
			return parsedSelector{}, fmt.Errorf("githubissues: malformed selector clause %q (want key:value)", tok)
		}
		key := tok[:colon]
		val := strings.TrimSpace(tok[colon+1:])
		if val == "" {
			return parsedSelector{}, fmt.Errorf("githubissues: selector clause %q has empty value", tok)
		}
		switch key {
		case "label":
			if _, dup := seenLabel[val]; dup {
				return parsedSelector{}, fmt.Errorf("githubissues: duplicate `label:%s` clause in selector %q", val, raw)
			}
			seenLabel[val] = struct{}{}
			out.labels = append(out.labels, val)
		case "state":
			if gotState {
				return parsedSelector{}, fmt.Errorf("githubissues: duplicate `state:` clause in selector %q", raw)
			}
			out.state = val
			gotState = true
		default:
			return parsedSelector{}, fmt.Errorf("githubissues: unknown selector clause %q (allowed: label, state)", key)
		}
	}
	if len(out.labels) == 0 {
		return parsedSelector{}, errors.New("githubissues: selector missing required `label:<name>` clause")
	}
	return out, nil
}

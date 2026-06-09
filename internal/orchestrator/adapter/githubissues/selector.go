package githubissues

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultSelectorState pins the default GH issue state the adapter polls when cfg.Selector omits the `state:` clause.
const DefaultSelectorState = "open"

type parsedSelector struct {
	label string
	state string
}

func defaultSelector() parsedSelector {
	return parsedSelector{label: AutonomousLabel, state: DefaultSelectorState}
}

// parseSelector accepts `label:<name>` and `state:<value>` space-separated. Reject duplicates + whitespace-only values so a typo cannot silently fall back to the default. Closes #1067.
func parseSelector(raw string) (parsedSelector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSelector(), nil
	}
	out := defaultSelector()
	gotLabel, gotState := false, false
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
			if gotLabel {
				return parsedSelector{}, fmt.Errorf("githubissues: duplicate `label:` clause in selector %q", raw)
			}
			out.label = val
			gotLabel = true
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
	if !gotLabel {
		return parsedSelector{}, errors.New("githubissues: selector missing required `label:<name>` clause")
	}
	return out, nil
}

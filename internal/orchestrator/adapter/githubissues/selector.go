// Package githubissues — selector.go parses the regatta.yaml::spec_adapter.selector grammar.
// Closes #1067: dead-code field becomes operator-controllable label+state retarget.
package githubissues

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultSelectorState pins the default GH issue state the adapter polls when cfg.Selector omits the `state:` clause.
const DefaultSelectorState = "open"

// parsedSelector pins the label + state the adapter filters on; populated at NewGitHubIssues from cfg.Selector.
type parsedSelector struct {
	label string
	state string
}

// defaultSelector preserves pre-#1067 behavior (label:autonomous state:open) when cfg.Selector is empty.
func defaultSelector() parsedSelector {
	return parsedSelector{label: AutonomousLabel, state: DefaultSelectorState}
}

// parseSelector accepts the minimal grammar `label:<name>` and `state:<value>` space-separated. Other tokens or empty clause values fail closed so a typo cannot silently fall back to the default.
func parseSelector(raw string) (parsedSelector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSelector(), nil
	}
	out := defaultSelector()
	gotLabel := false
	for _, tok := range strings.Fields(raw) {
		colon := strings.IndexByte(tok, ':')
		if colon <= 0 || colon == len(tok)-1 {
			return parsedSelector{}, fmt.Errorf("githubissues: malformed selector clause %q (want key:value)", tok)
		}
		key := tok[:colon]
		val := tok[colon+1:]
		switch key {
		case "label":
			out.label = val
			gotLabel = true
		case "state":
			out.state = val
		default:
			return parsedSelector{}, fmt.Errorf("githubissues: unknown selector clause %q (allowed: label, state)", key)
		}
	}
	if !gotLabel {
		return parsedSelector{}, errors.New("githubissues: selector missing required `label:<name>` clause")
	}
	return out, nil
}

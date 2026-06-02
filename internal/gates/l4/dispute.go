package l4

import (
	"os"
	"regexp"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// DefaultSecondOpinionModel is the alt-model the gate escalates to
// when the PR body disputes a finding. Defaults to Opus 4.7 to
// escalate from the Sonnet 4.6 primary — disagreement signal is the
// load-bearing reason to spend the extra tokens.
const DefaultSecondOpinionModel = "claude-opus-4-7"

// EnvSecondOpinionModel is the env-var escape hatch for the alt model.
// Yaml `gates.l4.second_opinion_model` still wins when set; env only
// fills the gap when yaml leaves it empty.
const EnvSecondOpinionModel = "REGATTA_GATES_L4_SECOND_OPINION_MODEL"

// disputeMarker matches one [L4-DISPUTE] line. The capture group is
// everything after the marker through end-of-line; ParseDisputes
// then splits that span on commas to tolerate "id1, id2" shorthand.
var disputeMarker = regexp.MustCompile(`(?m)^\s*\[L4-DISPUTE\]\s+(.+?)\s*$`)

// ParseDisputes extracts disputed finding IDs from a PR body. Markers
// take the shape `[L4-DISPUTE] L4-<CAT>-<SLUG>` per the binding-spec
// followup notes (issue #353). Multiple markers or comma-separated
// IDs both flatten into one slice; absent marker returns nil.
func ParseDisputes(body string) []string {
	if body == "" {
		return nil
	}
	matches := disputeMarker.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		for _, raw := range strings.Split(m[1], ",") {
			id := strings.TrimSpace(raw)
			if id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// intersect returns the subset of disputed finding IDs that actually
// appear in the primary findings list. A dispute for an ID the gate
// never raised is a no-op — no need to spend tokens re-confirming
// something that does not exist.
func intersect(disputed []string, primary []schemas.Finding) []string {
	if len(disputed) == 0 || len(primary) == 0 {
		return nil
	}
	have := make(map[string]bool, len(primary))
	for _, f := range primary {
		have[f.ID] = true
	}
	out := make([]string, 0, len(disputed))
	for _, id := range disputed {
		if have[id] {
			out = append(out, id)
		}
	}
	return out
}

// mergeDisputed drops a disputed primary finding iff the second
// opinion did NOT confirm it (no finding with the same ID). Findings
// not under dispute are preserved verbatim — second-opinion findings
// for non-disputed IDs are ignored to keep the merge deterministic.
func mergeDisputed(primary []schemas.Finding, disputed []string, second []schemas.Finding) []schemas.Finding {
	if len(disputed) == 0 {
		return primary
	}
	disputedSet := make(map[string]bool, len(disputed))
	for _, id := range disputed {
		disputedSet[id] = true
	}
	confirmed := make(map[string]bool, len(second))
	for _, f := range second {
		if disputedSet[f.ID] {
			confirmed[f.ID] = true
		}
	}
	out := make([]schemas.Finding, 0, len(primary))
	for _, f := range primary {
		if disputedSet[f.ID] && !confirmed[f.ID] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ResolveSecondOpinionModel applies the same yaml > env > default
// precedence as ResolveModel but for the alt-model escape hatch.
func ResolveSecondOpinionModel(yamlVal string) string {
	if yamlVal != "" {
		return yamlVal
	}
	if env := os.Getenv(EnvSecondOpinionModel); env != "" {
		return env
	}
	return DefaultSecondOpinionModel
}

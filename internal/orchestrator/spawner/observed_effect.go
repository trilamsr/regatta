package spawner

import "sort"

// ObservedSignal is a single side-effect observation from a tool call;
// Kind is the field the surprise detector folds against
// DeclaredEffectClass (spec §3.7).
type ObservedSignal struct {
	Kind     string
	Path     string
	Endpoint string
	USDMicro int64
}

// CollectObservedEffects returns the sorted, deduped Kind set; empty input yields a non-nil zero-length slice so callers can rely on len() == 0.
func CollectObservedEffects(sigs []ObservedSignal) []string {
	seen := map[string]struct{}{}
	for _, s := range sigs {
		if s.Kind == "" {
			continue
		}
		seen[s.Kind] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

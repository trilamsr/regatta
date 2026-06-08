package spawner

import "sort"

// ObservedSignal is one side-effect observation from a tool call. Kind
// is the only field the surprise detector folds against the run's
// DeclaredEffectClass envelope (spec §3.7); Path/Endpoint/USDMicro are
// kept on the struct for downstream consumers that want the raw record.
type ObservedSignal struct {
	Kind     string
	Path     string
	Endpoint string
	USDMicro int64
}

// CollectObservedEffects returns the sorted, deduped set of Kind values
// across sigs. Returns an empty (non-nil) slice when input is empty so
// the substrate consumer can rely on len() == 0 detection.
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

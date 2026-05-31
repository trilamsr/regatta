// CheckReachability enforces spec §3.3 rule 2c: every feature whose
// DefaultNext is non-nil must have that target transitively reachable
// from itself via the forward closure of outgoing Edges + DefaultNext
// links.
//
// Why: without this gate an operator can author a brief where every
// predicated edge resolves false at runtime and the default routes to
// a node no edge actually targets. The scheduler would then have no
// path to enqueue the default node, deadlocking the program.
//
// Algorithm: forward BFS from each feature with a non-nil DefaultNext,
// stepping over (a) outgoing Edge.To and (b) the visited feature's own
// DefaultNext when set. A self-loop on DefaultNext (A.DefaultNext=A)
// is rejected because BFS starts AFTER the source — the source itself
// is not considered "reached".
//
// The structural gates in ValidateV2 (unknown_target, missing_default,
// predicate compile/type) run BEFORE this check, so by the time we
// arrive every feature ID referenced by an edge or default_next is
// known to exist in the brief.

package program

import (
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// CheckReachability returns ErrEdgeUnreachable if any feature's
// DefaultNext cannot be reached from the feature itself by walking
// outgoing edges and per-node DefaultNext links forward.
func (p *ProgramBriefV2) CheckReachability() error {
	byID := make(map[string]*PlannedFeatureV2, len(p.FeaturesV2))
	for i := range p.FeaturesV2 {
		byID[p.FeaturesV2[i].ID] = &p.FeaturesV2[i]
	}
	for i := range p.FeaturesV2 {
		src := &p.FeaturesV2[i]
		if src.DefaultNext == nil {
			continue
		}
		target := *src.DefaultNext
		if !forwardReachable(src.ID, target, byID) {
			return fmt.Errorf("%w: feature %s default_next=%s",
				orchestrator.ErrEdgeUnreachable, src.ID, target)
		}
	}
	return nil
}

// forwardReachable runs BFS from start (exclusive — start itself is not
// counted as reached) and returns true once target is visited. Steps
// over outgoing Edge.To plus the visited node's DefaultNext.
func forwardReachable(start, target string, byID map[string]*PlannedFeatureV2) bool {
	visited := make(map[string]bool, len(byID))
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		f, ok := byID[cur]
		if !ok {
			continue
		}
		for _, e := range f.Edges {
			if e.To == target {
				return true
			}
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, e.To)
			}
		}
		if f.DefaultNext != nil {
			dn := *f.DefaultNext
			// Skip the source's own DefaultNext on the first hop — we are
			// checking whether the default is reachable via edges, not
			// trivially via itself.
			if cur == start {
				continue
			}
			if dn == target {
				return true
			}
			if !visited[dn] {
				visited[dn] = true
				queue = append(queue, dn)
			}
		}
	}
	return false
}

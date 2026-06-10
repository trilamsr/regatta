package scheduler

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// FileScopeExtractor projects the predicted file-paths a candidate WorkItem
// will touch. nil disables the collision check (pre-#1065 behavior).
type FileScopeExtractor func(state.WorkItem) []string

// pathBacktickRE matches backtick-quoted source paths under the four
// repo-root dirs that own load-bearing code/config. README.md and other
// out-of-tree mentions stay out of scope so doc-only references in an
// acceptance bullet do not trigger a false collision.
var pathBacktickRE = regexp.MustCompile(
	"`((?:cmd|internal|scripts|docs|contracts|Makefile\\.d)/[^`\\s]+)`",
)

// pathVerbRE matches `edit|add|modify <path>` forms inside `[planned]`
// acceptance bullets (issue spec c1). Path must start with one of the
// same load-bearing prefixes and end before whitespace / colon / period.
var pathVerbRE = regexp.MustCompile(
	`(?i)\b(?:edit|add|modify)\s+((?:cmd|internal|scripts|docs|contracts|Makefile\.d)/[^\s,;:` + "`" + `]+)`,
)

// DefaultFileScopeExtractor parses wi.AcceptanceJSON's `{"body":"..."}`
// envelope (#1092 stopgap) for backtick-quoted load-bearing paths and
// `edit|add|modify <path>` verb forms. Empty envelope or no matches
// return nil — the collision check then defaults to allow.
func DefaultFileScopeExtractor(wi state.WorkItem) []string {
	body := extractBody(wi.AcceptanceJSON)
	if body == "" {
		return nil
	}
	seen := map[string]struct{}{}
	for _, m := range pathBacktickRE.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = struct{}{}
	}
	for _, m := range pathVerbRE.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// extractBody pulls the `body` field out of the github_issues adapter's
// AcceptanceJSON envelope. Falls back to the raw string when the envelope
// is absent (brief-source rows) so the extractor still works.
func extractBody(acceptanceJSON string) string {
	if acceptanceJSON == "" {
		return ""
	}
	var doc struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(acceptanceJSON), &doc); err == nil && doc.Body != "" {
		return doc.Body
	}
	return acceptanceJSON
}

// fileScopeCollides returns true when active and incoming share any path
// OR when one is a directory-prefix of the other. A trailing-slash entry
// in active (e.g. `internal/orchestrator/spawner/`) collides with any
// candidate path under that directory — matches the spec's "shared
// package" rule (c6).
func fileScopeCollides(active, incoming []string) bool {
	if len(active) == 0 || len(incoming) == 0 {
		return false
	}
	for _, a := range active {
		for _, b := range incoming {
			if pathsOverlap(a, b) {
				return true
			}
		}
	}
	return false
}

// pathsOverlap is the per-pair predicate. Equal paths collide; a
// trailing-slash dir on either side collides with anything under it.
func pathsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	if strings.HasSuffix(a, "/") && strings.HasPrefix(b, a) {
		return true
	}
	if strings.HasSuffix(b, "/") && strings.HasPrefix(a, b) {
		return true
	}
	return false
}

// buildActiveFileScopes walks every in-flight agent and collects its
// predicted file scope keyed by agent ID. Active = any non-terminal state
// already counted by lane occupancy. Returns nil when the extractor is
// nil OR the schedulerDB does not expose GetWorkItem (no upcast available).
func (s *Scheduler) buildActiveFileScopes(ctx context.Context) map[int64]activeScope {
	if s.cfg.FileScopeExtractor == nil {
		return nil
	}
	getter, ok := s.db.(workItemGetter)
	if !ok {
		return nil
	}
	agents, err := s.db.ListAgentsByState(ctx, activeStates...)
	if err != nil {
		s.log.Warn("scheduler.file_scope_active_list_failed", string(obs.KeyErr), err.Error())
		return nil
	}
	out := make(map[int64]activeScope, len(agents))
	for _, a := range agents {
		wi, err := getter.GetWorkItem(ctx, a.WorkItemID)
		if err != nil {
			continue
		}
		paths := s.cfg.FileScopeExtractor(wi)
		if len(paths) == 0 {
			continue
		}
		out[a.ID] = activeScope{workItemID: a.WorkItemID, paths: paths}
	}
	return out
}

// activeScope pairs the in-flight agent's work-item id (for log payloads)
// with its predicted file paths.
type activeScope struct {
	workItemID string
	paths      []string
}

// scopeConflict carries the conflicting agent's identity for the
// scheduler.file_scope_collision_deferred event payload.
type scopeConflict struct {
	agentID    int64
	workItemID string
}

// detectScopeCollision returns the first in-flight agent whose scope
// overlaps the candidate, along with the overlapping paths. ok=false when
// the candidate is clear OR the extractor returned no scope. Same-lane
// constraint: only agents whose lane equals the candidate's lane count
// (a shared file across lanes is rarer in practice and would otherwise
// over-defer cross-lane work).
func (s *Scheduler) detectScopeCollision(w state.WorkItem, active, reserved map[int64]activeScope) (scopeConflict, []string, bool) {
	incoming := s.cfg.FileScopeExtractor(w)
	if len(incoming) == 0 {
		return scopeConflict{}, nil, false
	}
	for _, src := range []map[int64]activeScope{active, reserved} {
		for agentID, sc := range src {
			overlap := overlapPaths(sc.paths, incoming)
			if len(overlap) > 0 {
				return scopeConflict{agentID: agentID, workItemID: sc.workItemID}, overlap, true
			}
		}
	}
	return scopeConflict{}, nil, false
}

// overlapPaths returns the subset of (a,b) pairs that collide under
// pathsOverlap. Caller uses non-empty result as a boolean AND log payload.
func overlapPaths(active, incoming []string) []string {
	var out []string
	for _, a := range active {
		for _, b := range incoming {
			if pathsOverlap(a, b) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

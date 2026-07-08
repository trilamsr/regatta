package scheduler

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// FileScopeExtractor projects predicted file paths for a candidate; nil disables the collision check (pre-#1065 behavior).
type FileScopeExtractor func(state.WorkItem) []string

var pathBacktickRE = regexp.MustCompile(
	"`((?:cmd|internal|scripts|docs|contracts|Makefile\\.d)/[^`\\s]+)`",
)

var pathVerbRE = regexp.MustCompile(
	`(?i)\b(?:edit|add|modify)\s+((?:cmd|internal|scripts|docs|contracts|Makefile\.d)/[^\s,;:` + "`" + `]+)`,
)

// DefaultFileScopeExtractor parses the github_issues adapter body envelope (#1092 stopgap) for backtick paths + edit|add|modify verb forms.
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

// fileScopeCollides returns true when active and incoming share any path or one is a directory-prefix of the other (c6 shared-package rule).
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

// pathsOverlap returns true on exact-path match, directory-prefix containment, or shared parent package (c6 shared-package rule — two files in the same dir collide to prevent cascade-rebase storms).
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
	return parentDir(a) == parentDir(b)
}

func parentDir(p string) string {
	if strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/")
	}
	return path.Dir(p)
}

// buildActiveFileScopes maps in-flight agents to their predicted file scopes; nil when the extractor is unwired or no WorkItem source is available. Consults tc.workItems first so N active agents cost 0 GetWorkItem calls when the tick-scoped snapshot is populated (#1359); falls back to per-id GetWorkItem otherwise.
func (s *Scheduler) buildActiveFileScopes(ctx context.Context, tc *tickCtx) map[int64]activeScope {
	if s.cfg.FileScopeExtractor == nil {
		return nil
	}
	getter, hasGetter := s.db.(workItemGetter)
	snapshotOnly := tc != nil && tc.workItems != nil
	if !hasGetter && !snapshotOnly {
		return nil
	}
	agents, err := s.db.ListAgentsByState(ctx, activeStates...)
	if err != nil {
		s.log.Warn("scheduler.file_scope_active_list_failed", string(obs.KeyErr), err.Error())
		return nil
	}
	out := make(map[int64]activeScope, len(agents))
	for _, a := range agents {
		wi, ok := lookupWorkItem(ctx, tc, getter, hasGetter, a.WorkItemID)
		if !ok {
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

// lookupWorkItem returns the tc.workItems hit when present, otherwise falls back to getter.GetWorkItem — the per-id path the snapshot replaces (#1359). ok=false when both miss so the caller drops the agent from downstream sets.
func lookupWorkItem(ctx context.Context, tc *tickCtx, getter workItemGetter, hasGetter bool, id string) (state.WorkItem, bool) {
	if tc != nil && tc.workItems != nil {
		if wi, hit := tc.workItems[id]; hit {
			return wi, true
		}
	}
	if !hasGetter {
		return state.WorkItem{}, false
	}
	wi, err := getter.GetWorkItem(ctx, id)
	if err != nil {
		return state.WorkItem{}, false
	}
	return wi, true
}

type activeScope struct {
	workItemID string
	paths      []string
}

type scopeConflict struct {
	agentID    int64
	workItemID string
}

// detectScopeCollision returns the first in-flight agent whose scope overlaps the candidate and the overlapping paths; ok=false when clear.
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

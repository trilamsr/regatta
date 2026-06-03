package program

import (
	"context"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// projectV2ToV1 fills the embedded ProgramBrief.Features slice from
// FeaturesV2, translating Edges into DependsOnFeatures so the v1-shaped
// Sync pipeline accepts a v2 brief unchanged. Outputs schema +
// predicate metadata stays on the returned ProgramBriefV2 (callers
// needing v2 fields use LoadAndVerifyBriefV2).
//
// V2 edges use outgoing semantics (e.From == owning feature ID); to
// reconstruct the v1 incoming-edge DependsOnFeatures view we reverse-
// index: for each edge U -> D in U.Edges, append U to D's
// DependsOnFeatures.
func projectV2ToV1(v2 *ProgramBriefV2) *ProgramBrief {
	out := v2.ProgramBrief
	out.Features = make([]PlannedFeature, len(v2.FeaturesV2))
	idxByID := make(map[string]int, len(v2.FeaturesV2))
	for i, f := range v2.FeaturesV2 {
		out.Features[i] = f.PlannedFeature
		idxByID[f.ID] = i
	}
	seenDep := make([]map[string]bool, len(v2.FeaturesV2))
	for i := range out.Features {
		seenDep[i] = map[string]bool{}
		for _, d := range out.Features[i].DependsOnFeatures {
			seenDep[i][d] = true
		}
	}
	for _, f := range v2.FeaturesV2 {
		for _, e := range f.Edges {
			j, ok := idxByID[e.To]
			if !ok {
				continue
			}
			if seenDep[j][e.From] {
				continue
			}
			out.Features[j].DependsOnFeatures = append(out.Features[j].DependsOnFeatures, e.From)
			seenDep[j][e.From] = true
		}
	}
	return &out
}

// materialiseEdges lowers a v2 brief's Edges + DefaultNext into
// work_item_edges rows and upserts them in one transaction. v1 briefs
// have no edge data and skip this pass.
//
// Existing rows preserve their fired/fired_against state — re-plans
// that mutate predicate text refresh predicate_cel + on_skip but the
// scheduler's eval result for the prior predicate stays put. Operators
// who want clean re-eval tombstone the program manually.
func (b *BriefLoader) materialiseEdges(ctx context.Context, v2 *ProgramBriefV2, at time.Time) error {
	if v2 == nil {
		return nil
	}
	var rows []state.EdgeRow
	for _, f := range v2.FeaturesV2 {
		for _, e := range f.Edges {
			rows = append(rows, state.EdgeRow{
				ProgramID:    v2.ProgramID,
				FromID:       e.From,
				ToID:         e.To,
				PredicateCEL: e.Predicate,
				IsDefault:    false,
				OnSkip:       string(skipOrDefault(e.OnSkip)),
			})
		}
		if f.DefaultNext != nil {
			rows = append(rows, state.EdgeRow{
				ProgramID:    v2.ProgramID,
				FromID:       f.ID,
				ToID:         *f.DefaultNext,
				PredicateCEL: "",
				IsDefault:    true,
				OnSkip:       string(SkipCascade),
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if err := b.db.UpsertEdgesAt(ctx, v2.ProgramID, rows, at); err != nil {
		return fmt.Errorf("brief_loader: upsert edges for %s: %w", v2.ProgramID, err)
	}
	b.log.Info(string(obs.EventBriefEdgesMaterialised),
		string(obs.KeyProgramID), v2.ProgramID, "count", len(rows))
	return nil
}

// skipOrDefault canonicalises an empty SkipMode to SkipCascade so every
// persisted edge row carries a non-empty on_skip value.
func skipOrDefault(s SkipMode) SkipMode {
	if s == "" {
		return SkipCascade
	}
	return s
}

// featureAcceptanceSnapshot snapshots the criteria a feature fulfills
// at brief-ingestion time so downstream cascade-soft archival keeps
// the child self-describing — operators reading a stale archived row
// can still see what acceptance bar it was meant to clear (spec §2.4).
func featureAcceptanceSnapshot(f PlannedFeature, byFulfilled map[string]string) []PlanCriterion {
	out := make([]PlanCriterion, 0, len(f.Fulfills))
	for _, fid := range f.Fulfills {
		out = append(out, PlanCriterion{ID: fid, Text: byFulfilled[fid]})
	}
	return out
}

package program

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// reachBrief builds a minimal v2 brief skeleton with the supplied
// features. The header fields satisfy ValidateV2's structural gates so
// CheckReachability is the gate under test.
func reachBrief(features []PlannedFeatureV2) *ProgramBriefV2 {
	return &ProgramBriefV2{
		ProgramBrief: ProgramBrief{
			SchemaVersion:    2,
			ProgramID:        "m-reachreach00",
			ParentWorkItemID: "PROG-ROOT",
			ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "x"}},
			PlannerModelID:   "test:model",
		},
		FeaturesV2: features,
	}
}

func TestReachability_HappyPath(t *testing.T) {
	b := reachBrief([]PlannedFeatureV2{
		{
			PlannedFeature: PlannedFeature{ID: "F-A", Title: "a", Fulfills: []string{"c1"}},
			Edges: []Edge{
				{From: "F-A", To: "F-B"},
				{From: "F-A", To: "F-C", Predicate: `out.severity == "high"`},
			},
			OutputsSchema: &OutputsSchema{
				Type: "object",
				Properties: map[string]*OutputsSchema{
					"severity": {Type: "string"},
				},
			},
			DefaultNext: strPtr("F-B"),
		},
		{PlannedFeature: PlannedFeature{ID: "F-B", Title: "b"}},
		{PlannedFeature: PlannedFeature{ID: "F-C", Title: "c"}},
	})
	if err := b.CheckReachability(); err != nil {
		t.Fatalf("reachable default_next must pass, got %v", err)
	}
}

func TestReachability_DefaultNextOrphan(t *testing.T) {
	b := reachBrief([]PlannedFeatureV2{
		{
			PlannedFeature: PlannedFeature{ID: "F-A", Title: "a", Fulfills: []string{"c1"}},
			Edges: []Edge{
				{From: "F-A", To: "F-B", Predicate: `out.x == 1`},
			},
			OutputsSchema: &OutputsSchema{
				Type:       "object",
				Properties: map[string]*OutputsSchema{"x": {Type: "int"}},
			},
			DefaultNext: strPtr("F-Z"),
		},
		{PlannedFeature: PlannedFeature{ID: "F-B", Title: "b"}},
		{PlannedFeature: PlannedFeature{ID: "F-Z", Title: "z"}},
	})
	err := b.CheckReachability()
	if !errors.Is(err, orchestrator.ErrEdgeUnreachable) {
		t.Fatalf("got %v; want errors.Is(ErrEdgeUnreachable)", err)
	}
}

func TestReachability_TransitiveReachable(t *testing.T) {
	b := reachBrief([]PlannedFeatureV2{
		{
			PlannedFeature: PlannedFeature{ID: "F-A", Title: "a", Fulfills: []string{"c1"}},
			Edges:          []Edge{{From: "F-A", To: "F-B"}},
			DefaultNext:    strPtr("F-C"),
		},
		{
			PlannedFeature: PlannedFeature{ID: "F-B", Title: "b"},
			Edges:          []Edge{{From: "F-B", To: "F-C"}},
		},
		{PlannedFeature: PlannedFeature{ID: "F-C", Title: "c"}},
	})
	if err := b.CheckReachability(); err != nil {
		t.Fatalf("transitive reachability must pass, got %v", err)
	}
}

func TestReachability_DefaultNextSelfLoop(t *testing.T) {
	b := reachBrief([]PlannedFeatureV2{
		{
			PlannedFeature: PlannedFeature{ID: "F-A", Title: "a", Fulfills: []string{"c1"}},
			Edges:          []Edge{{From: "F-A", To: "F-B"}},
			DefaultNext:    strPtr("F-A"),
		},
		{PlannedFeature: PlannedFeature{ID: "F-B", Title: "b"}},
	})
	err := b.CheckReachability()
	if !errors.Is(err, orchestrator.ErrEdgeUnreachable) {
		t.Fatalf("self-loop default_next must reject, got %v", err)
	}
}

func TestReachability_DisconnectedFeature(t *testing.T) {
	b := reachBrief([]PlannedFeatureV2{
		{
			PlannedFeature: PlannedFeature{ID: "F-A", Title: "a", Fulfills: []string{"c1"}},
			Edges:          []Edge{{From: "F-A", To: "F-B"}},
			DefaultNext:    strPtr("F-B"),
		},
		{PlannedFeature: PlannedFeature{ID: "F-B", Title: "b"}},
		{PlannedFeature: PlannedFeature{ID: "F-X", Title: "orphan"}},
	})
	if err := b.CheckReachability(); err != nil {
		t.Fatalf("orphan feature without default_next must not trip reachability, got %v", err)
	}
}

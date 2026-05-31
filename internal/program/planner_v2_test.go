package program

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func strPtr(s string) *string { return &s }

// validV2 returns a minimal well-formed v2 brief for use as a base in
// rejection tests.
func validV2(t *testing.T) *ProgramBriefV2 {
	t.Helper()
	return &ProgramBriefV2{
		ProgramBrief: ProgramBrief{
			SchemaVersion:    2,
			ProgramID:        "m-aaaaaaaaaaaa",
			ParentWorkItemID: "PROG-ROOT",
			ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "ship it"}},
			PlannerModelID:   "test:model",
		},
		FeaturesV2: []PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type: "object",
					Properties: map[string]*OutputsSchema{
						"severity": {Type: "string", Enum: []any{"high", "low"}},
					},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.severity == "high"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-B"),
			},
			{
				PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate", Fulfills: nil},
			},
		},
	}
}

func TestValidateV2_RejectsAll(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*ProgramBriefV2)
		want error
	}{
		{
			name: "unknown_target",
			mut:  func(b *ProgramBriefV2) { b.FeaturesV2[0].Edges[0].To = "F-NOPE" },
			want: orchestrator.ErrEdgeUnknownTarget,
		},
		{
			name: "missing_default",
			mut:  func(b *ProgramBriefV2) { b.FeaturesV2[0].DefaultNext = nil },
			want: orchestrator.ErrEdgeMissingDefault,
		},
		{
			name: "predicate_compile",
			mut:  func(b *ProgramBriefV2) { b.FeaturesV2[0].Edges[0].Predicate = "out.severity ==" },
			want: orchestrator.ErrPredicateCompile,
		},
		{
			name: "predicate_unknown_field",
			mut:  func(b *ProgramBriefV2) { b.FeaturesV2[0].Edges[0].Predicate = `out.nope == "x"` },
			want: orchestrator.ErrPredicateUnknownField,
		},
		{
			name: "predicate_type_mismatch",
			mut:  func(b *ProgramBriefV2) { b.FeaturesV2[0].Edges[0].Predicate = `out.severity == 42` },
			want: orchestrator.ErrPredicateTypeMismatch,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := validV2(t)
			tc.mut(b)
			err := b.ValidateV2()
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v; want errors.Is(%v)", err, tc.want)
			}
		})
	}
}

func TestValidateV2_HappyPath(t *testing.T) {
	if err := validV2(t).ValidateV2(); err != nil {
		t.Fatalf("validV2 must validate: %v", err)
	}
}

func TestValidateV2_LowerV1Equivalent(t *testing.T) {
	v1 := &ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        "m-bbbbbbbbbbbb",
		ParentWorkItemID: "PROG",
		ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "x"}},
		PlannerModelID:   "test",
		Features: []PlannedFeature{
			{ID: "F-A", Title: "a", Fulfills: []string{"c1"}, DependsOnFeatures: nil},
			{ID: "F-B", Title: "b", Fulfills: nil, DependsOnFeatures: []string{"F-A"}},
		},
	}
	lowered := LowerV1ToV2(v1)
	if lowered.SchemaVersion != 2 {
		t.Fatalf("lowered SchemaVersion got %d want 2", lowered.SchemaVersion)
	}
	if len(lowered.FeaturesV2[1].Edges) == 0 {
		t.Fatalf("F-B should have 1 lowered edge from F-A")
	}
	if lowered.FeaturesV2[1].Edges[0].Predicate != "" {
		t.Fatalf("lowered edge must be unconditional, got %q", lowered.FeaturesV2[1].Edges[0].Predicate)
	}
}

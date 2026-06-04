package program

import (
	"context"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// StubPlanner is the offline ModelClient used by `regatta program plan
// -planner=stub` and e2e tests: emits one feature per parent acceptance
// criterion so the planner pipeline (Validate + Sign) exercises end-to-end
// without an Anthropic key. Not byte-deterministic — Run() stamps fresh
// ProducedAt + random ProgramID.
type StubPlanner struct{}

// NewStubPlanner returns a StubPlanner.
func NewStubPlanner() *StubPlanner { return &StubPlanner{} }

// ModelID returns "stub:v1"; the "stub:" prefix mirrors "anthropic:" so PlannerModelID always names its provider.
func (s *StubPlanner) ModelID() string { return "stub:v1" }

// Plan emits one feature per parent criterion. Returns only model-owned fields (Features); the pipeline stamps program_id, parent_criteria, planner_model_id, produced_at.
func (s *StubPlanner) Plan(_ context.Context, parent schemas.WorkItem) (*ProgramBrief, error) {
	if len(parent.AcceptanceCriteria) == 0 {
		return nil, errors.New("stub planner: parent has no acceptance criteria")
	}
	feats := make([]PlannedFeature, 0, len(parent.AcceptanceCriteria))
	for i, c := range parent.AcceptanceCriteria {
		feats = append(feats, PlannedFeature{
			ID:       fmt.Sprintf("F-STUB-%02d", i+1),
			Title:    fmt.Sprintf("stub feature for %s", c.ID),
			Fulfills: []string{c.ID},
		})
	}
	return &ProgramBrief{Features: feats}, nil
}

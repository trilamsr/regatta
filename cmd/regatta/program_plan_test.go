package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// TestRunProgramPlanRejectsNonProgramKind verifies that `regatta program plan`
// refuses a work item whose Kind is not "program". The planner is normative
// on this; silent acceptance would let a feature item slip into the planner
// and produce a brief with no meaningful decomposition.
func TestRunProgramPlanRejectsNonProgramKind(t *testing.T) {
	t.Setenv("HMAC_KEY", "dummy")
	dir := t.TempDir()
	path := filepath.Join(dir, "wi.json")
	item := schemas.WorkItem{
		ID:    "WI-1",
		Title: "leaf",
		Kind:  schemas.KindFeature,
		AcceptanceCriteria: []schemas.Criterion{
			{ID: "c1", Text: "do thing"},
		},
		Status: schemas.StatusPlanned,
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rc := runProgramPlan([]string{"-hmac-key-env=HMAC_KEY", path})
	if rc != 2 {
		t.Fatalf("expected exit 2 for non-program kind, got %d", rc)
	}
}

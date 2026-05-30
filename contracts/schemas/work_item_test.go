package schemas

import "testing"

func TestWorkItemKindValues(t *testing.T) {
	t.Parallel()
	if KindFeature != "feature" {
		t.Errorf("KindFeature = %q, want %q", KindFeature, "feature")
	}
	if KindProgram != "program" {
		t.Errorf("KindProgram = %q, want %q", KindProgram, "program")
	}
}

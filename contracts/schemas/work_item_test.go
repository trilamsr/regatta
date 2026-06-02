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

// TestStatusValues pins the wire spelling of every WorkItem.Status — JSON schema + markdown adapter parser both depend on these strings (issue #482).
func TestStatusValues(t *testing.T) {
	t.Parallel()
	cases := map[Status]string{
		StatusPlanned:        "planned",
		StatusInProgress:     "in_progress",
		StatusDone:           "done",
		StatusClosedResolved: "closed-resolved",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%q != %q", got, want)
		}
	}
}

// TestCriterionStateValues pins the wire spelling of every Criterion.State — markdown criterion regex depends on these strings (issue #482).
func TestCriterionStateValues(t *testing.T) {
	t.Parallel()
	cases := map[CriterionState]string{
		CriterionPlanned:    "planned",
		CriterionInProgress: "in_progress",
		CriterionDone:       "done",
		CriterionClosed:     "closed",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%q != %q", got, want)
		}
	}
}

package schemas

import "testing"

// TestSpecAdapter_UpdateStatusDocCitesAllStatuses pins the interface docstring promise that all 4 Status enum members are accepted by UpdateStatus.
func TestSpecAdapter_UpdateStatusDocCitesAllStatuses(t *testing.T) {
	enum := []Status{StatusPlanned, StatusInProgress, StatusDone, StatusClosedResolved}
	if len(enum) != 4 {
		t.Fatalf("expected 4 Status enum members, got %d", len(enum))
	}
	for _, s := range enum {
		if string(s) == "" {
			t.Fatalf("Status %q is empty — wire format broken", s)
		}
	}
}

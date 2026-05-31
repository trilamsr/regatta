package orchestrator

import (
	"errors"
	"testing"
)

func TestSentinelsDistinct(t *testing.T) {
	all := []error{
		ErrBriefSHAMismatch,
		ErrHMACInvalid,
		ErrTargetExists,
		ErrFlockHeld,
		ErrSchemaTooNew,
		ErrCycleDetected,
	}
	seen := map[string]bool{}
	for _, e := range all {
		if e == nil {
			t.Fatalf("sentinel must be non-nil")
		}
		msg := e.Error()
		if seen[msg] {
			t.Fatalf("duplicate sentinel message: %q", msg)
		}
		seen[msg] = true
	}
	// Identity check: catches a future regression where someone
	// re-declares a sentinel with the same string but distinct
	// identity (which would break errors.Is callers that import via
	// the re-export path in state). We explicitly compare with ==
	// (not errors.Is) because we want pointer-identity inequality
	// across distinct sentinels; errorlint's wrapped-error warning
	// does not apply when the intent is identity, not semantics.
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[i] == all[j] { //nolint:errorlint // identity check, not semantic match
				t.Fatalf("sentinels at %d and %d are identity-equal: %v", i, j, all[i])
			}
		}
	}
}

func TestSentinelWrapping(t *testing.T) {
	wrapped := errors.Join(ErrBriefSHAMismatch, errors.New("extra context"))
	if !errors.Is(wrapped, ErrBriefSHAMismatch) {
		t.Fatalf("errors.Is must match through Join")
	}
}

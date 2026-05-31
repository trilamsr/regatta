package orchestrator

import (
	"errors"
	"testing"
)

func TestSentinelsDistinct(t *testing.T) {
	// Only the orchestrator-package-native sentinels live here.
	// state.ErrSchemaTooNew / state.ErrCycleDetected /
	// state.ErrInvalidTransition / state.ErrLockHeld are owned by
	// the state package and imported directly by callers.
	all := []error{
		ErrBriefSHAMismatch,
		ErrHMACInvalid,
		ErrTargetExists,
		ErrFlockHeld,
		ErrPlannerPromptMissing,
		ErrCascadeNonConverging,
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
	// identity. We compare with == (not errors.Is) because we want
	// pointer-identity inequality across distinct sentinels.
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

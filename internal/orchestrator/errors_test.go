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
}

func TestSentinelWrapping(t *testing.T) {
	wrapped := errors.Join(ErrBriefSHAMismatch, errors.New("extra context"))
	if !errors.Is(wrapped, ErrBriefSHAMismatch) {
		t.Fatalf("errors.Is must match through Join")
	}
}

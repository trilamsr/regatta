package orchestrator

import (
	"errors"
	"testing"
)

func TestSentinelsV2Distinct(t *testing.T) {
	all := []error{
		ErrPredicateCompile,
		ErrPredicateUnknownField,
		ErrPredicateTypeMismatch,
		ErrEdgeMissingDefault,
		ErrEdgeUnknownTarget,
		ErrJournalNotFound,
	}
	seen := map[string]bool{}
	for _, e := range all {
		if e == nil {
			t.Fatalf("v2 sentinel must be non-nil")
		}
		if seen[e.Error()] {
			t.Fatalf("duplicate sentinel: %q", e.Error())
		}
		seen[e.Error()] = true
	}
}

func TestSentinelsV2Wrappable(t *testing.T) {
	wrapped := errors.Join(ErrPredicateCompile, errors.New("CEL syntax error at pos 3"))
	if !errors.Is(wrapped, ErrPredicateCompile) {
		t.Fatalf("errors.Is must match through Join")
	}
}

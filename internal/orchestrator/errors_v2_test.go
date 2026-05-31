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
		ErrEdgeUnreachable,
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
	cases := []struct {
		name string
		sent error
	}{
		{"ErrPredicateCompile", ErrPredicateCompile},
		{"ErrPredicateUnknownField", ErrPredicateUnknownField},
		{"ErrPredicateTypeMismatch", ErrPredicateTypeMismatch},
		{"ErrEdgeMissingDefault", ErrEdgeMissingDefault},
		{"ErrEdgeUnknownTarget", ErrEdgeUnknownTarget},
		{"ErrEdgeUnreachable", ErrEdgeUnreachable},
		{"ErrJournalNotFound", ErrJournalNotFound},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			wrapped := errors.Join(tc.sent, errors.New("caller context"))
			if !errors.Is(wrapped, tc.sent) {
				t.Fatalf("errors.Is must match %s through Join", tc.name)
			}
		})
	}
}

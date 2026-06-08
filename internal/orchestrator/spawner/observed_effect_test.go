package spawner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestObservedEffect_EmptyForNoSignals asserts CollectObservedEffects returns an empty slice for nil input (#operator-console-S0).
func TestObservedEffect_EmptyForNoSignals(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects(nil)
	if len(got) != 0 {
		t.Errorf("expected empty set, got %v", got)
	}
}

// TestObservedEffect_FilesystemWriteOnly asserts a single filesystem-write signal maps to a one-element kind set (#operator-console-S0).
func TestObservedEffect_FilesystemWriteOnly(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects([]ObservedSignal{
		{Kind: "filesystem-write", Path: "foo.go"},
	})
	want := []string{"filesystem-write"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
}

// TestObservedEffect_MultipleClassesDeduped asserts repeated kinds collapse and output is sorted (#operator-console-S0).
func TestObservedEffect_MultipleClassesDeduped(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects([]ObservedSignal{
		{Kind: "filesystem-write", Path: "a.go"},
		{Kind: "filesystem-write", Path: "b.go"},
		{Kind: "gh-mutation", Endpoint: "PATCH /repos/x/y/pulls/1"},
		{Kind: "cost-delta", USDMicro: 1234},
	})
	want := []string{"cost-delta", "filesystem-write", "gh-mutation"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
}

// TestObservedEffect_UnknownKindPreserved asserts forward-compat kinds pass through verbatim (#operator-console-S0).
func TestObservedEffect_UnknownKindPreserved(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects([]ObservedSignal{
		{Kind: "future-kind-xyz"},
	})
	want := []string{"future-kind-xyz"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
}

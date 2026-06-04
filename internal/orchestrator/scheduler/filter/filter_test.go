package filter_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/scheduler/filter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func wi(id string) state.WorkItem { return state.WorkItem{ID: id} }

func keepAll(_ state.WorkItem) (struct{}, bool) { return struct{}{}, true }

// TestApply_KeepsAllWhenAllGated asserts every wi gated + Evaluate=keep returns input order (#251).
func TestApply_KeepsAllWhenAllGated(t *testing.T) {
	in := []state.WorkItem{wi("a"), wi("b"), wi("c")}
	out, err := filter.Apply(context.Background(), filter.Pass[struct{}]{
		Name:    "all-keep",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, _ state.WorkItem, _ struct{}) (bool, error) {
			return true, nil
		},
	}, in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got, want := ids(out), []string{"a", "b", "c"}; !equalSlice(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestApply_SkipsUngated asserts Resolve=false bypasses Evaluate verbatim (#251).
func TestApply_SkipsUngated(t *testing.T) {
	in := []state.WorkItem{wi("a"), wi("b")}
	calls := 0
	out, err := filter.Apply(context.Background(), filter.Pass[struct{}]{
		Name: "none-gated",
		Resolve: func(_ state.WorkItem) (struct{}, bool) {
			return struct{}{}, false
		},
		Evaluate: func(_ context.Context, _ state.WorkItem, _ struct{}) (bool, error) {
			calls++
			return false, nil
		},
	}, in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 0 {
		t.Fatalf("Evaluate called %d times for ungated wi", calls)
	}
	if got, want := ids(out), []string{"a", "b"}; !equalSlice(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestApply_DropsOnEvaluateFalse asserts keep=false removes wi while preserving order (#251).
func TestApply_DropsOnEvaluateFalse(t *testing.T) {
	in := []state.WorkItem{wi("a"), wi("b"), wi("c"), wi("d")}
	out, err := filter.Apply(context.Background(), filter.Pass[struct{}]{
		Name:    "drop-even",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			return w.ID == "a" || w.ID == "c", nil
		},
	}, in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got, want := ids(out), []string{"a", "c"}; !equalSlice(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestApply_HaltsOnEvaluateError asserts first error short-circuits with nil slice (#251).
func TestApply_HaltsOnEvaluateError(t *testing.T) {
	sentinel := errors.New("halt")
	in := []state.WorkItem{wi("a"), wi("b"), wi("c")}
	out, err := filter.Apply(context.Background(), filter.Pass[struct{}]{
		Name:    "err-on-b",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			if w.ID == "b" {
				return false, sentinel
			}
			return true, nil
		},
	}, in)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v want wraps sentinel", err)
	}
	if out != nil {
		t.Fatalf("want nil slice on error, got %v", ids(out))
	}
}

// TestApply_EmptySpawnable asserts empty input returns empty output without Evaluate call (#251).
func TestApply_EmptySpawnable(t *testing.T) {
	calls := 0
	out, err := filter.Apply(context.Background(), filter.Pass[struct{}]{
		Name:    "empty",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, _ state.WorkItem, _ struct{}) (bool, error) {
			calls++
			return true, nil
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("want empty, got %v", ids(out))
	}
	if calls != 0 {
		t.Fatalf("Evaluate called %d times for empty input", calls)
	}
}

// TestApply_PreservesOrder asserts interleaved keep/drop preserves input order (#251).
func TestApply_PreservesOrder(t *testing.T) {
	in := []state.WorkItem{wi("a"), wi("b"), wi("c"), wi("d"), wi("e")}
	keepSet := map[string]bool{"a": true, "c": true, "e": true}
	out, err := filter.Apply(context.Background(), filter.Pass[struct{}]{
		Name:    "TFTFT",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			return keepSet[w.ID], nil
		},
	}, in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got, want := ids(out), []string{"a", "c", "e"}; !equalSlice(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestApplyComposition asserts chaining two passes matches union of predicates without info loss (#251 §5).
func TestApplyComposition(t *testing.T) {
	in := []state.WorkItem{wi("a"), wi("b"), wi("c"), wi("d")}
	p1 := filter.Pass[struct{}]{
		Name:    "p1",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			return w.ID != "b", nil
		},
	}
	p2 := filter.Pass[struct{}]{
		Name:    "p2",
		Resolve: keepAll,
		Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			return w.ID != "d", nil
		},
	}
	mid, err := filter.Apply(context.Background(), p1, in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := filter.Apply(context.Background(), p2, mid)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids(out), []string{"a", "c"}; !equalSlice(got, want) {
		t.Fatalf("composed got %v want %v", got, want)
	}
}

// TestFilterApply_PropertyIdempotent asserts Apply(Apply(x))==Apply(x) and reorder-invariant kept-set (#251 §5).
func TestFilterApply_PropertyIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		ids := rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,4}`), 0, 12).Draw(rt, "ids")
		mod := rapid.IntRange(1, 7).Draw(rt, "mod")
		in := make([]state.WorkItem, 0, len(ids))
		for _, id := range ids {
			in = append(in, wi(id))
		}
		ev := func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			h := 0
			for _, b := range []byte(w.ID) {
				h += int(b)
			}
			return h%mod != 0, nil
		}
		p := filter.Pass[struct{}]{Name: "prop", Resolve: keepAll, Evaluate: ev}
		first, err := filter.Apply(context.Background(), p, in)
		if err != nil {
			rt.Fatal(err)
		}
		second, err := filter.Apply(context.Background(), p, first)
		if err != nil {
			rt.Fatal(err)
		}
		if !equalSlice(ids2(first), ids2(second)) {
			rt.Fatalf("idempotence: %v vs %v", ids2(first), ids2(second))
		}
		reversed := make([]state.WorkItem, len(in))
		for i, w := range in {
			reversed[len(in)-1-i] = w
		}
		rev, err := filter.Apply(context.Background(), p, reversed)
		if err != nil {
			rt.Fatal(err)
		}
		a := ids2(first)
		b := ids2(rev)
		sort.Strings(a)
		sort.Strings(b)
		if !equalSlice(a, b) {
			rt.Fatalf("kept-set reorder: %v vs %v", a, b)
		}
	})
}

// TestFilterApply_PropertyCrossPassCommutes asserts A∘B == B∘A on disjoint-key drop sets (#704 R3).
func TestFilterApply_PropertyCrossPassCommutes(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		ids := rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,4}`), 0, 12).Draw(rt, "ids")
		dropA := rapid.SliceOfDistinct(rapid.StringMatching(`[a-z]{1,4}`), rapid.ID[string]).Draw(rt, "dropA")
		dropB := rapid.SliceOfDistinct(rapid.StringMatching(`[a-z]{1,4}`), rapid.ID[string]).Draw(rt, "dropB")
		setA := map[string]bool{}
		for _, id := range dropA {
			setA[id] = true
		}
		setB := map[string]bool{}
		for _, id := range dropB {
			if !setA[id] {
				setB[id] = true
			}
		}
		in := make([]state.WorkItem, 0, len(ids))
		for _, id := range ids {
			in = append(in, wi(id))
		}
		pA := filter.Pass[struct{}]{
			Name:    "A",
			Resolve: keepAll,
			Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
				return !setA[w.ID], nil
			},
		}
		pB := filter.Pass[struct{}]{
			Name:    "B",
			Resolve: keepAll,
			Evaluate: func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
				return !setB[w.ID], nil
			},
		}
		ab, err := filter.Apply(context.Background(), pA, in)
		if err != nil {
			rt.Fatal(err)
		}
		ab, err = filter.Apply(context.Background(), pB, ab)
		if err != nil {
			rt.Fatal(err)
		}
		ba, err := filter.Apply(context.Background(), pB, in)
		if err != nil {
			rt.Fatal(err)
		}
		ba, err = filter.Apply(context.Background(), pA, ba)
		if err != nil {
			rt.Fatal(err)
		}
		if !equalSlice(ids2(ab), ids2(ba)) {
			rt.Fatalf("A∘B=%v B∘A=%v", ids2(ab), ids2(ba))
		}
	})
}

// FuzzApply_OrderIndependentVerdict asserts kept-set invariant under input reorder (#251 §5).
func FuzzApply_OrderIndependentVerdict(f *testing.F) {
	f.Add("a,b,c,d,e", uint8(3))
	f.Add("x,y,z", uint8(2))
	f.Add("", uint8(1))
	f.Fuzz(func(t *testing.T, csv string, dropMod uint8) {
		mod := int(dropMod%7) + 1
		ids := splitCSV(csv)
		in := make([]state.WorkItem, 0, len(ids))
		for _, id := range ids {
			in = append(in, wi(id))
		}
		ev := func(_ context.Context, w state.WorkItem, _ struct{}) (bool, error) {
			h := 0
			for _, b := range []byte(w.ID) {
				h += int(b)
			}
			return h%mod != 0, nil
		}
		p := filter.Pass[struct{}]{Name: "fuzz", Resolve: keepAll, Evaluate: ev}
		out1, err := filter.Apply(context.Background(), p, in)
		if err != nil {
			t.Fatal(err)
		}
		reversed := make([]state.WorkItem, len(in))
		for i, w := range in {
			reversed[len(in)-1-i] = w
		}
		out2, err := filter.Apply(context.Background(), p, reversed)
		if err != nil {
			t.Fatal(err)
		}
		a := ids2(out1)
		b := ids2(out2)
		sort.Strings(a)
		sort.Strings(b)
		if !equalSlice(a, b) {
			t.Fatalf("kept-set differs: %v vs %v", a, b)
		}
	})
}

func ids(ws []state.WorkItem) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.ID
	}
	return out
}

func ids2(ws []state.WorkItem) []string { return ids(ws) }

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

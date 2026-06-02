package l4

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Same (diff,spec,model) on the second Run reuses the prior findings.
func TestL4_FindingsCache_SecondInvocationHits(t *testing.T) {
	var calls atomic.Int32
	base := func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
		calls.Add(1)
		return InvokeResponse{
			Findings: []schemas.Finding{{
				ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingHigh,
				Claim: "off-by-one in tick counter",
			}},
			PromptSHA: "sha-1",
		}, nil
	}
	cached := NewCachedInvoker(base, 8)
	cfg := Config{GateID: "l4_adversarial", Model: DefaultModel, Invoker: cached}
	in := Input{PRSHA: "deadbeef", RunID: "run-A", Diff: "@@ +1 -1 @@", Spec: "spec body"}

	gr1, err := Run(context.Background(), cfg, in)
	if err != nil {
		t.Fatalf("first run: unexpected error: %v", err)
	}
	gr2, err := Run(context.Background(), cfg, Input{PRSHA: "cafef00d", RunID: "run-B", Diff: in.Diff, Spec: in.Spec})
	if err != nil {
		t.Fatalf("second run: unexpected error: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("Invoker call count: got %d, want 1 (cache should serve the second run)", got)
	}
	if len(gr1.Findings) != len(gr2.Findings) || gr1.Findings[0].ID != gr2.Findings[0].ID {
		t.Fatalf("findings drift across cache hit: gr1=%+v gr2=%+v", gr1.Findings, gr2.Findings)
	}
}

// Distinct (diff,spec,model) keys all miss and re-invoke the model.
func TestL4_FindingsCache_DifferentKeyMisses(t *testing.T) {
	var calls atomic.Int32
	base := func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
		calls.Add(1)
		return InvokeResponse{PromptSHA: "sha-" + req.Input.Diff}, nil
	}
	cached := NewCachedInvoker(base, 8)
	cfg := Config{GateID: "l4_adversarial", Model: DefaultModel, Invoker: cached}

	if _, err := Run(context.Background(), cfg, Input{PRSHA: "a", RunID: "1", Diff: "diff-A", Spec: "S"}); err != nil {
		t.Fatalf("run A: %v", err)
	}
	// Different diff -> different key -> miss.
	if _, err := Run(context.Background(), cfg, Input{PRSHA: "a", RunID: "2", Diff: "diff-B", Spec: "S"}); err != nil {
		t.Fatalf("run B: %v", err)
	}
	// Different spec -> different key -> miss.
	if _, err := Run(context.Background(), cfg, Input{PRSHA: "a", RunID: "3", Diff: "diff-A", Spec: "S2"}); err != nil {
		t.Fatalf("run C: %v", err)
	}

	if got := calls.Load(); got != 3 {
		t.Fatalf("Invoker call count: got %d, want 3 (every distinct key must miss)", got)
	}
}

// Model is part of the cache key -- swapping model misses.
func TestL4_FindingsCache_ModelInKey(t *testing.T) {
	var calls atomic.Int32
	base := func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
		calls.Add(1)
		return InvokeResponse{}, nil
	}
	cached := NewCachedInvoker(base, 8)
	in := Input{PRSHA: "z", RunID: "1", Diff: "D", Spec: "S"}
	if _, err := Run(context.Background(), Config{Model: "claude-sonnet-4-6", Invoker: cached}, in); err != nil {
		t.Fatalf("sonnet run: %v", err)
	}
	if _, err := Run(context.Background(), Config{Model: "claude-opus-4-7", Invoker: cached}, in); err != nil {
		t.Fatalf("opus run: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("Invoker call count: got %d, want 2 (model swap must miss)", got)
	}
}

// Errors are uncached so a transient outage cannot poison later runs.
func TestL4_FindingsCache_ErrorsNotCached(t *testing.T) {
	var calls atomic.Int32
	base := func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
		n := calls.Add(1)
		if n == 1 {
			return InvokeResponse{}, context.DeadlineExceeded
		}
		return InvokeResponse{Findings: []schemas.Finding{{ID: "L4-OK"}}}, nil
	}
	cached := NewCachedInvoker(base, 8)
	cfg := Config{GateID: "g", Model: DefaultModel, Invoker: cached}
	in := Input{PRSHA: "p", RunID: "1", Diff: "D", Spec: "S"}

	// First Run hits an error path; Run swallows the invoke error
	// and emits an advisory result (gate.go:99-107). Cache must not
	// memoize that failure.
	if _, err := Run(context.Background(), cfg, in); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := Run(context.Background(), cfg, in); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("Invoker call count: got %d, want 2 (errors must not be cached)", got)
	}
}

// LRU eviction drops the oldest key past capacity so it re-invokes.
func TestL4_FindingsCache_LRUEviction(t *testing.T) {
	var calls atomic.Int32
	base := func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
		calls.Add(1)
		return InvokeResponse{}, nil
	}
	cached := NewCachedInvoker(base, 2)
	cfg := Config{Model: DefaultModel, Invoker: cached}
	mkInput := func(d string) Input { return Input{PRSHA: d, RunID: d, Diff: d, Spec: "S"} }

	for _, d := range []string{"A", "B", "C", "A"} { // A evicted by C, re-invoked at index 3
		if _, err := Run(context.Background(), cfg, mkInput(d)); err != nil {
			t.Fatalf("run %s: %v", d, err)
		}
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("Invoker call count: got %d, want 4 (A,B,C,A-re-invoke after eviction)", got)
	}
}

// Capacity <= 0 disables caching and passes through every call.
func TestL4_FindingsCache_ZeroCapacityIsPassthrough(t *testing.T) {
	var calls atomic.Int32
	base := func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
		calls.Add(1)
		return InvokeResponse{}, nil
	}
	cached := NewCachedInvoker(base, 0)
	cfg := Config{Model: DefaultModel, Invoker: cached}
	in := Input{PRSHA: "p", RunID: "1", Diff: "D", Spec: "S"}
	for i := 0; i < 3; i++ {
		if _, err := Run(context.Background(), cfg, in); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("Invoker call count: got %d, want 3 (capacity=0 must passthrough)", got)
	}
}

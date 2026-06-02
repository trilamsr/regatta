//go:build !race

package otel_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	obsotel "github.com/trilamsr/regatta/internal/obs/otel"
)

// TestBridge_Handle_BothLegs_OverheadUnder5Micros guards the issue #175 budget by running the 5-attr both-legs bench and asserting steady-state ns/op stays under bridgeBenchBudgetNsPerOp.
func TestBridge_Handle_BothLegs_OverheadUnder5Micros(t *testing.T) {
	if testing.Short() {
		t.Skip("perf guard skipped under -short")
	}
	// Steady-state ns/op is measured by the bench framework, not by an
	// ad-hoc time.Since loop, because the framework adapts b.N until the
	// inner loop dominates wall-clock — that is the only signal that
	// excludes setup costs. testing.Benchmark drives the same closure
	// the CLI bench tool uses, so this assertion is contract-equivalent
	// to running `go test -bench`.
	//
	// Guarded with !race: the race detector adds ~5-20x overhead on the
	// otelslog leg (mutex-heavy SDK processor chain), which would force
	// the budget into the 50µs range and lose its production signal.
	// `make check` runs `-race`; this assertion runs in the bench lane
	// (`make bench` / `go test -run=Overhead`), preserving the 5µs
	// production contract without flaking the race gate.
	res := testing.Benchmark(func(b *testing.B) {
		primary := slog.NewTextHandler(io.Discard, nil)
		lp, _ := newTestProvider()
		defer func() { _ = lp.Shutdown(context.Background()) }()

		bridge := obsotel.NewBridgeHandler(primary, "regatta-bench", obsotel.WithLoggerProvider(lp))
		lg := slog.New(bridge)
		attrs := typicalAttrs(5)
		ctx := context.Background()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lg.LogAttrs(ctx, slog.LevelInfo, string(obs.EventTickStarted), attrs...)
		}
	})
	nsPerOp := res.NsPerOp()
	t.Logf("bridge 5-attr overhead: %d ns/op (budget %d)", nsPerOp, bridgeBenchBudgetNsPerOp)
	if nsPerOp > bridgeBenchBudgetNsPerOp {
		t.Fatalf("bridge overhead %d ns/op > budget %d ns/op (issue #175)", nsPerOp, bridgeBenchBudgetNsPerOp)
	}
}

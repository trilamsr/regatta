package otel_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/trilamsr/regatta/internal/obs"
	obsotel "github.com/trilamsr/regatta/internal/obs/otel"
)

// bridgeBenchBudgetNsPerOp pins the per-record overhead budget for the
// bridge with both legs (slog + OTel sdklog) wired. Issue #175 fixed the
// budget at <5µs for the typical (5-attr) record so operators can rely
// on the fan-out staying off the critical path of hot loops (tick,
// scheduler, dispatch). Larger record sizes are reported for visibility
// but only the 5-attr point is asserted — that mirrors the production
// shape of the obs corpus (KeyProgramID + KeyLane + KeyReason + a couple
// of int64s is the modal record).
const bridgeBenchBudgetNsPerOp = 5000

// bridgeBenchBudgetAllocsPerOp pins the per-record alloc budget for the
// 5-attr both-legs path. Issue #467: bench reports allocs/op but the
// guard previously asserted only ns/op, so a defensive copy that
// doubled allocs (e.g. fmt.Sprintf in the hot path) could still pass
// the ns/op gate and ship a silent regression. Current measured value
// is 2 allocs/op (bridge wrapper + otelslog record build); ceiling at
// 3 leaves headroom for one incidental alloc without permitting the
// 2x growth regression shape #467 calls out.
const bridgeBenchBudgetAllocsPerOp = 3

// countingProcessor is a zero-alloc sdklog.Processor used by the bench
// and perf-guard paths to isolate bridge fan-out cost from sink cost.
// memProcessor (bridge_test.go) clones each record and appends under a
// mutex to an unbounded slice — for b.N≥600K that adds ~150MB of heap
// growth, per-record clone, and mutex serialisation into the timed
// loop, none of which reflect bridge overhead (issue #466). The
// counting variant keeps OnEmit allocation-free and lock-free so the
// bench measures the bridge, not the sink.
type countingProcessor struct {
	emitted atomic.Int64
}

func (p *countingProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (p *countingProcessor) OnEmit(_ context.Context, _ *sdklog.Record) error {
	p.emitted.Add(1)
	return nil
}

func (p *countingProcessor) Shutdown(context.Context) error   { return nil }
func (p *countingProcessor) ForceFlush(context.Context) error { return nil }

// newBenchProvider builds a LoggerProvider wired to countingProcessor; returned alongside the provider so callers can assert emission count without paying the memProcessor clone/append cost (issue #466).
func newBenchProvider() (*sdklog.LoggerProvider, *countingProcessor) {
	p := &countingProcessor{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(p))
	return lp, p
}

// BenchmarkBridge_Handle_BothLegs measures per-record overhead with both legs (discard slog + counting sdklog) wired at 0/5/20 attrs; the 5-attr ns/op and allocs/op budgets are asserted by the sibling perf-guard test (issue #175).
func BenchmarkBridge_Handle_BothLegs(b *testing.B) {
	sizes := []struct {
		name  string
		attrs []slog.Attr
	}{
		{"0attrs", nil},
		{"5attrs", typicalAttrs(5)},
		{"20attrs", typicalAttrs(20)},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			// Discard primary keeps the primary leg's I/O cost negligible
			// so the measurement isolates the bridge fan-out + otelslog
			// translation cost, which is the variable the budget targets.
			primary := slog.NewTextHandler(io.Discard, nil)
			lp, _ := newBenchProvider()
			b.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

			bridge := obsotel.NewBridgeHandler(primary, "regatta-bench", lp)
			lg := slog.New(bridge)
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				lg.LogAttrs(ctx, slog.LevelInfo, string(obs.EventTickStarted), sz.attrs...)
			}
			b.StopTimer()
		})
	}
}

// typicalAttrs returns n attrs mirroring the obs corpus's modal record (string + int64 mix); deterministic so the bench is reproducible across runs.
func typicalAttrs(n int) []slog.Attr {
	if n == 0 {
		return nil
	}
	keys := []string{
		string(obs.KeyProgramID),
		string(obs.KeyLane),
		string(obs.KeyReason),
		string(obs.KeyAttemptCount),
		string(obs.KeyPeriodStart),
	}
	out := make([]slog.Attr, n)
	for i := 0; i < n; i++ {
		k := keys[i%len(keys)]
		if i%2 == 0 {
			out[i] = slog.String(k, "v")
		} else {
			out[i] = slog.Int64(k, int64(i))
		}
	}
	return out
}

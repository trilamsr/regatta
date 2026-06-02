package otel_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

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

// BenchmarkBridge_Handle_BothLegs measures per-record overhead with both legs (discard slog + sdklog) wired at 0/5/20 attrs; the 5-attr budget is asserted by the sibling perf-guard test (issue #175).
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
			lp, _ := newTestProvider()
			b.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

			bridge := obsotel.NewBridgeHandler(primary, "regatta-bench", obsotel.WithLoggerProvider(lp))
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

package rejectionrouter

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// scopeName is the OTel instrumentation scope this package emits
// metrics under. Mirrors the spend / l4 retrofit from T0a Config.Meter retrofit so the
// per-scope query slice ("rejection_router") stays grep-able and the
// cardinality lint walks the same surface across the orchestrator.
const scopeName = "github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"

// ResolveMeter returns the Config.Meter or falls back to the global
// provider's scoped meter. Fallback is lazy so a test that swaps the
// global provider after Config construction still picks up the swap.
// Mirrors spend.Config.ResolveMeter.
func (c Config) ResolveMeter() metric.Meter {
	return resolveMeter(c.Meter)
}

func resolveMeter(m metric.Meter) metric.Meter {
	if m != nil {
		return m
	}
	return otel.Meter(scopeName)
}

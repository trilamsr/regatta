package spend

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// scopeName — OTel instrumentation scope; tests + cost-governor wire
// instruments against this name so the per-scope query slice stays
// grep-able.
const scopeName = "github.com/trilamsr/regatta/internal/cost/spend"

// Config holds the DI handles RecordCall and the cost-governor draw
// path need to emit metrics under a stable scope. WriteOptions owns
// per-call HMAC + clock; Config owns the cross-call telemetry seam
// A-T0a wires for A-T1 to extend.
type Config struct {
	// Meter resolves lazily at ResolveMeter() so a global provider
	// swap (test noop injection) takes effect on the next call.
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or falls back lazily.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

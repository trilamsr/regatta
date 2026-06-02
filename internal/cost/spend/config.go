package spend

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// scopeName is the OTel instrumentation scope the spend package emits
// metrics under. Tests and the cost-governor wire instruments against
// this name so the per-scope query slice ("spend") stays grep-able.
const scopeName = "github.com/trilamsr/regatta/internal/cost/spend"

// Config carries the dependency-injected handles RecordCall and the
// cost-governor draw path need to emit metrics under a stable scope.
// The struct intentionally stays thin — WriteOptions still owns the
// per-call HMAC + clock injection; Config owns the cross-call telemetry
// seam A-T0a wires for A-T1 (cost-governor metrics) to extend.
//
// W6 Config.Tracer pattern: nil Meter falls back to the global provider
// so callers that do not opt into telemetry get a no-cost noop wire.
type Config struct {
	// Meter is the OTel instrument factory. Nil resolves to
	// otel.Meter(scopeName) at the first ResolveMeter() call so the
	// global MeterProvider Setup wires (or a noop when Setup was
	// skipped) wins by default.
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. The fallback is lazy so callers
// can swap the global provider after Config construction (e.g. tests
// injecting a noop provider) and still pick up the swap on the next
// call.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

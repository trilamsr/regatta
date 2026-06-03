package obs

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// MeterScope is the typed alias for an OTel instrumentation scope name.
// One const block below pins every scope regatta dashboards key on, so
// a rename here is a one-line diff that touches every prod call site
// transitively — the inverse of the pre-Wave-B2 state where 13 string
// literals could drift apart silently.
type MeterScope string

// MeterScope* are the canonical scope names. The string value of each
// const MUST equal its historical otel.Meter("...") argument because
// dashboard queries grep on that exact text; the
// TestMeterScope_ConstsMatchHistoricalValues lint pins this invariant.
const (
	MeterScopeOrchestrator         MeterScope = "orchestrator"
	MeterScopeScheduler            MeterScope = "scheduler"
	MeterScopeSchedulerFallback    MeterScope = "scheduler-fallback"
	MeterScopeReaper               MeterScope = "reaper"
	MeterScopeSpawner              MeterScope = "spawner"
	MeterScopeAdaptersync          MeterScope = "adaptersync"
	MeterScopeAlarmwebhook         MeterScope = "alarmwebhook"
	MeterScopeAlarmwebhookFallback MeterScope = "alarmwebhook-fallback"
	MeterScopeObsTriggers          MeterScope = "obs/triggers"
	MeterScopeObsAdversarial       MeterScope = "obs/adversarial"
	MeterScopeCostCap              MeterScope = "internal/cost/cap"
	MeterScopeProgram              MeterScope = "program"
	MeterScopeSubstrate            MeterScope = "orchestrator/state/substrate"
)

// Meter returns the global MeterProvider's meter bound to scope.
// Centralizing this call lets the meter-scope lint enforce that
// production code never embeds a raw string-literal scope name.
func Meter(scope MeterScope) metric.Meter {
	return otel.Meter(string(scope))
}

// ResolveMeter returns stored when non-nil, else the global provider's
// meter at fallback scope. Encapsulates the "nil-falls-back-lazily"
// pattern every Config.ResolveMeter helper previously open-coded so a
// post-construct provider swap (test noop injection) wins on the next
// call without per-package reinvention.
func ResolveMeter(stored metric.Meter, fallback MeterScope) metric.Meter {
	if stored != nil {
		return stored
	}
	return Meter(fallback)
}

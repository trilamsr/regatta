package obs

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// MeterScope is the typed alias for an OTel instrumentation scope name.
// Centralising the names lets a rename touch every prod call site
// transitively — the inverse of the pre-Wave-B2 state where 13 string
// literals could drift apart silently.
type MeterScope string

// Canonical scope names. Each string value MUST equal its historical
// otel.Meter("...") argument — dashboard queries grep on the exact
// text; TestMeterScope_ConstsMatchHistoricalValues pins the invariant.
const (
	MeterScopeOrchestrator         MeterScope = "orchestrator"
	MeterScopeScheduler            MeterScope = "scheduler"
	MeterScopeSchedulerFallback    MeterScope = "scheduler-fallback"
	MeterScopeReaper               MeterScope = "reaper"
	MeterScopeSpawner              MeterScope = "spawner"
	MeterScopeAdaptersync          MeterScope = "adaptersync"
	MeterScopeAdaptersyncFallback  MeterScope = "adaptersync-fallback"
	MeterScopeAdapterGitHubIssues  MeterScope = "adapter/github_issues"
	MeterScopeAlerthook         MeterScope = "alerthook"
	MeterScopeAlerthookFallback MeterScope = "alerthook-fallback"
	MeterScopeObsTriggers          MeterScope = "obs/triggers"
	MeterScopeObsAdversarial       MeterScope = "obs/adversarial"
	MeterScopeCostCap              MeterScope = "internal/cost/cap"
	MeterScopeProgram              MeterScope = "program"
	MeterScopeSubstrate            MeterScope = "orchestrator/state/substrate"
)

// Meter returns the global MeterProvider's meter bound to scope. The
// meter-scope lint enforces that production code never embeds a raw
// string-literal scope name.
func Meter(scope MeterScope) metric.Meter {
	return otel.Meter(string(scope))
}

// ResolveMeter returns stored when non-nil, else the global provider's
// meter at fallback scope. Lazy fallback lets a post-construct provider
// swap (test noop injection) win on the next call.
func ResolveMeter(stored metric.Meter, fallback MeterScope) metric.Meter {
	if stored != nil {
		return stored
	}
	return Meter(fallback)
}

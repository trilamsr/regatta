// OBS Wave-B substrate-health instruments. T1 event-rate counter,
// T2 chain-break counter, T3 divergence counter live here so the four
// substrate-spine emitters share one meter-resolution seam and one
// closed-enum table. T4 replay-duration histogram lives in
// internal/history/replay_latency.go (different package boundary).
//
// Why package-level singletons: substrate has no Config struct (the
// spec assumes Wave-A A-T0b retrofit, not landed here). We resolve the
// meter once via sync.Once against the global provider; tests that
// need a custom meter use SetMeterForTesting. Lock-free atomic .Add()
// keeps AppendEvent on its sub-µs hot-path budget.

package substrate

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"

	"github.com/trilamsr/regatta/internal/obs"
)

// tagOther is the literal cardinality-bound fallthrough tag value
// shared by every closed-enum guard in this package. One constant
// keeps the dashboard panel + alert rule + emit-site in lockstep.
const tagOther = "other"

// validKinds is the closed-enum guard for the `kind` tag on the
// event-rate counter (T1). Any kind not in this map routes to literal
// "other" so a leaked typo cannot blow up cardinality. Mirrors
// AllKinds(); the lint test TestProgramKindEnum_Closed asserts parity.
var validKinds = map[EventKind]struct{}{
	KindNodeOutput:       {},
	KindFact:             {},
	KindApprovalEvent:    {},
	KindTokenSpend:       {},
	KindBudgetReconciled: {},
	KindGateVerdict:      {},
	KindHeartbeat:        {},
	KindBriefRejected:    {},
}

// validProgramKinds is the closed-enum guard for the divergence (T3)
// counter's `program_kind` tag. The set is intentionally small and
// matches the program-registry kinds documented in the spec §5;
// unknown values route to "other" so a future program-kind addition
// without a registry update cannot leak unbounded series.
var validProgramKinds = map[string]struct{}{
	"dag":  {},
	"task": {},
	"fold": {},
}

// kindOrOther sanitizes an EventKind for use as a metric tag value.
// Unknown kinds become "other" — never the raw string — so cardinality
// stays bounded even when a typo'd caller slips past the validator.
func kindOrOther(k EventKind) string {
	if _, ok := validKinds[k]; ok {
		return string(k)
	}
	return tagOther
}

// programKindOrOther sanitizes a program-kind string for the divergence
// counter. Unknown values become literal "other" so the cardinality
// budget (5 enums × 5 layers ≤ 25 cells) holds even under registry drift.
func programKindOrOther(pk string) string {
	if _, ok := validProgramKinds[pk]; ok {
		return pk
	}
	return tagOther
}

// instruments holds the three substrate counters. Lazy-initialised so
// AppendEvent does not pay the OTel SDK first-call cost on every test
// fixture; the sync.Once shrinks init to one mutex on the cold path.
type instruments struct {
	events     metric.Int64Counter
	chainBreak metric.Int64Counter
	divergence metric.Int64Counter
}

var (
	instrumentsOnce sync.Once
	instrumentsVal  *instruments
	meterMu         sync.RWMutex
	meterOverride   metric.Meter
)

// SetMeterForTesting overrides the substrate's package-level meter so
// tests can read counter values from a controlled SDK reader. Returns
// a restore closure the caller defers to undo the swap. Test-only;
// production callers MUST NOT use this — the global provider wins.
func SetMeterForTesting(m metric.Meter) func() {
	meterMu.Lock()
	prev := meterOverride
	meterOverride = m
	meterMu.Unlock()
	// Force re-init on next resolveInstruments() call.
	resetInstrumentsForTesting()
	return func() {
		meterMu.Lock()
		meterOverride = prev
		meterMu.Unlock()
		resetInstrumentsForTesting()
	}
}

// resetInstrumentsForTesting clears the sync.Once so the next call to
// resolveInstruments rebinds against the (possibly overridden) meter.
// Also resets the replay-histogram once so T4's instrument rebinds
// under the same swap. Test seam only.
func resetInstrumentsForTesting() {
	instrumentsOnce = sync.Once{}
	instrumentsVal = nil
	resetReplayHistogramForTesting()
}

// resolveMeter returns the test-override meter if set, else falls back
// to the global provider's substrate-scoped meter. Nil never escapes —
// the noop meter is a valid metric.Meter that absorbs .Add() calls
// without allocating, so callers never need a nil guard.
func resolveMeter() metric.Meter {
	meterMu.RLock()
	defer meterMu.RUnlock()
	if meterOverride != nil {
		return meterOverride
	}
	return obs.Meter(obs.MeterScopeSubstrate)
}

// resolveInstruments builds the three counters once per meter binding.
// Counter-creation errors land on the noop meter so AppendEvent never
// panics on a malformed scope name; the cost is silent metric loss,
// which the dashboard "metric missing" panel surfaces to the operator.
func resolveInstruments() *instruments {
	instrumentsOnce.Do(func() {
		m := resolveMeter()
		ev, err := m.Int64Counter("regatta.substrate.events.appended",
			metric.WithDescription("count of substrate events appended to the log"))
		if err != nil {
			ev, _ = noopmetric.NewMeterProvider().Meter("noop").Int64Counter("regatta.substrate.events.appended")
		}
		cb, err := m.Int64Counter("regatta.substrate.chain.break",
			metric.WithDescription("count of HMAC chain-break detections (read-path + sweeper)"))
		if err != nil {
			cb, _ = noopmetric.NewMeterProvider().Meter("noop").Int64Counter("regatta.substrate.chain.break")
		}
		dv, err := m.Int64Counter("regatta.substrate.divergence.detected",
			metric.WithDescription("count of replay-vs-recorded divergence detections"))
		if err != nil {
			dv, _ = noopmetric.NewMeterProvider().Meter("noop").Int64Counter("regatta.substrate.divergence.detected")
		}
		instrumentsVal = &instruments{events: ev, chainBreak: cb, divergence: dv}
	})
	return instrumentsVal
}

// recordEventAppended bumps the T1 events.appended counter. Called from
// AppendEvent on the post-INSERT success path so the counter reflects
// rows that actually landed, not rows the validator rejected. The
// closed-enum guard routes unknown kinds to "other" before the .Add().
func recordEventAppended(ctx context.Context, kind EventKind) {
	resolveInstruments().events.Add(ctx, 1,
		metric.WithAttributes(attribute.String("kind", kindOrOther(kind))))
}

// recordChainBreak bumps the T2 chain-break counter. Both wiring sites
// (read-path Verify and the background sweeper) route through this
// helper so the tag set stays consistent: one `event_kind` label,
// closed-enum guarded.
func recordChainBreak(ctx context.Context, kind EventKind) {
	resolveInstruments().chainBreak.Add(ctx, 1,
		metric.WithAttributes(attribute.String("event_kind", kindOrOther(kind))))
}

// recordDivergence bumps the T3 divergence counter. Called from the
// audit-table reader (divergence.go) on every new row. Both tags are
// enum-guarded; the reader passes the raw values from the audit row
// and the helper does the "other" fallthrough.
func recordDivergence(ctx context.Context, programKind, layer string) {
	resolveInstruments().divergence.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("program_kind", programKindOrOther(programKind)),
			attribute.String("layer", layerOrOther(layer)),
		))
}

// validLayers is the closed-enum guard for the divergence counter's
// `layer` tag. Mirrors the detector strings the audit table CHECK
// constraint pins (0011 migration) plus the spec §5 "other" overflow.
var validLayers = map[string]struct{}{
	"layer1_write": {},
	"layer1_read":  {},
	"layer2_test":  {},
	"layer3_cron":  {},
	"replay":       {},
	"audit":        {},
}

// layerOrOther sanitizes a layer string for the divergence counter.
// Bounded at the same 5-enum budget the spec §5 table promises.
func layerOrOther(l string) string {
	if _, ok := validLayers[l]; ok {
		return l
	}
	return tagOther
}

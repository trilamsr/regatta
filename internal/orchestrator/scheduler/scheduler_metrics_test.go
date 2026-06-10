package scheduler_test

import (
	"context"
	"sort"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// expectedSteps pins the spec §3 row 4 / §10 brief A-T3 enum to one place.
// gate_parallel_cap added by spec 2026-06-09 §3.1 (#1169).
var expectedSteps = []string{
	"dispatch",
	"fold",
	"gate_approval",
	"gate_cost",
	"gate_cost_cap",
	"gate_l0",
	"gate_l4",
	"gate_parallel_cap",
	"persist",
	"reaper",
}

func newReaderProvider(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	return r, mp
}

func seedPlannedExt(t *testing.T, db *state.DB, id, lane string) {
	t.Helper()
	w := state.WorkItem{
		ID: id, Kind: state.KindFeature, Title: id,
		Lane: lane, Status: state.WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, time.Now()); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// TestTick_EmitsLatencyHistogram pins the tick.latency_ms emission contract.
func TestTick_EmitsLatencyHistogram(t *testing.T) {
	r, mp := newReaderProvider(t)
	db := statetest.OpenDB(t)
	seedPlannedExt(t, db, "F-1", "server")

	s := scheduler.New(db, scheduler.Config{Meter: mp.Meter("scheduler-test")})
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	hist, ok := findHistogram(t, r, "regatta.scheduler.tick.latency_ms")
	if !ok {
		t.Fatalf("regatta.scheduler.tick.latency_ms not emitted")
	}
	if len(hist.DataPoints) != 1 {
		t.Fatalf("latency_ms DataPoints = %d, want 1", len(hist.DataPoints))
	}
	dp := hist.DataPoints[0]
	if dp.Count != 1 {
		t.Errorf("latency_ms Count = %d, want 1", dp.Count)
	}
	if dp.Attributes.Len() != 0 {
		t.Errorf("latency_ms attributes = %d; spec forbids labels", dp.Attributes.Len())
	}
}

// TestTick_EmitsStepDurationHistogramAllEnums pins the per-step enum coverage contract.
func TestTick_EmitsStepDurationHistogramAllEnums(t *testing.T) {
	r, mp := newReaderProvider(t)
	db := statetest.OpenDB(t)
	seedPlannedExt(t, db, "F-1", "server")

	s := scheduler.New(db, scheduler.Config{Meter: mp.Meter("scheduler-test")})
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	hist, ok := findHistogram(t, r, "regatta.scheduler.tick.step_duration_ms")
	if !ok {
		t.Fatalf("regatta.scheduler.tick.step_duration_ms not emitted")
	}

	got := make([]string, 0, len(hist.DataPoints))
	for _, dp := range hist.DataPoints {
		v, ok := dp.Attributes.Value("step")
		if !ok {
			t.Errorf("step_duration_ms datapoint missing step attribute")
			continue
		}
		if dp.Attributes.Len() != 1 {
			t.Errorf("step_duration_ms attrs = %d; want exactly 1 (step)", dp.Attributes.Len())
		}
		got = append(got, v.AsString())
	}
	sort.Strings(got)
	if !equalStrings(got, expectedSteps) {
		t.Errorf("step enums = %v, want %v", got, expectedSteps)
	}
}

// TestTick_StepLatencyEmitsOncePerTickPerStep pins the no-per-iteration-emission shape.
func TestTick_StepLatencyEmitsOncePerTickPerStep(t *testing.T) {
	r, mp := newReaderProvider(t)
	db := statetest.OpenDB(t)
	for _, id := range []string{"F-1", "F-2", "F-3"} {
		seedPlannedExt(t, db, id, "server")
	}

	s := scheduler.New(db, scheduler.Config{Meter: mp.Meter("scheduler-test")})
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	hist, ok := findHistogram(t, r, "regatta.scheduler.tick.step_duration_ms")
	if !ok {
		t.Fatalf("step_duration_ms not emitted")
	}
	if len(hist.DataPoints) != len(expectedSteps) {
		t.Errorf("DataPoints = %d, want %d (one per enum, NOT per work_item)", len(hist.DataPoints), len(expectedSteps))
	}
	for _, dp := range hist.DataPoints {
		if dp.Count != 1 {
			t.Errorf("step datapoint Count = %d, want 1 (one observation per tick per step)", dp.Count)
		}
	}
}

func findHistogram(t *testing.T, r *sdkmetric.ManualReader, name string) (metricdata.Histogram[float64], bool) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			return h, true
		}
	}
	return metricdata.Histogram[float64]{}, false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

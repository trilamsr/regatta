package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestScheduler_AdapterPollErrorCounter pins #889: List error on a MinPoll-gated poll increments regatta.scheduler.adapter_poll.errors_total with adapter_index attr.
func TestScheduler_AdapterPollErrorCounter(t *testing.T) {
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	clock := time.Unix(1_700_000_000, 0).UTC()
	clockFn := func() time.Time { return clock }
	db, err := state.OpenWithClock(context.Background(), state.DSN(filepath.Join(t.TempDir(), "poll_err_counter.db")), clockFn)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ad := &countingMinPollAdapter{minPoll: 5 * time.Second, listErr: schemas.ErrRateLimited}
	sch := New(db, Config{
		Clock:    clockFn,
		Adapters: []schemas.SpecAdapter{ad},
		Meter:    mp.Meter("scheduler-test"),
	})

	if _, err := sch.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var found bool
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "regatta.scheduler.adapter_poll.errors_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s: data is %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				v, ok := dp.Attributes.Value("adapter_index")
				if !ok {
					t.Errorf("datapoint missing adapter_index attribute")
					continue
				}
				if v.AsInt64() != 0 {
					t.Errorf("adapter_index = %d, want 0", v.AsInt64())
				}
				total += dp.Value
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("regatta.scheduler.adapter_poll.errors_total not emitted")
	}
	if total != 1 {
		t.Errorf("counter total = %d, want 1 (one List error on tick 1)", total)
	}
}

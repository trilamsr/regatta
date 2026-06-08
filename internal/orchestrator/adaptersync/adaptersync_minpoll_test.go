package adaptersync_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
)

type countingMinPollAdapter struct {
	calls   atomic.Int64
	minPoll time.Duration
	listErr error
}

func (c *countingMinPollAdapter) List(_ context.Context) ([]schemas.WorkItem, error) {
	c.calls.Add(1)
	return nil, c.listErr
}

func (c *countingMinPollAdapter) Capabilities() schemas.Capabilities {
	return schemas.Capabilities{MinPollInterval: c.minPoll}
}

func (c *countingMinPollAdapter) listCalls() int64 { return c.calls.Load() }

// TestSyncer_HonoursMinPollInterval_SkipsInsideBudget pins #888 §9.1 — Sync MUST short-circuit adapter.List inside MinPollInterval.
func TestSyncer_HonoursMinPollInterval_SkipsInsideBudget(t *testing.T) {
	const minPoll = 30 * time.Second
	ad := &countingMinPollAdapter{minPoll: minPoll}
	db := newSyncTestDB(t)
	s := mustNew(t, adaptersync.Config{Adapter: ad, DB: db})

	t0 := time.Unix(1_700_000_000, 0).UTC()
	if err := s.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync t0: %v", err)
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("after Sync t0: List calls=%d want 1 (zero-value lastPoll must fire)", got)
	}

	if err := s.Sync(context.Background(), t0.Add(10*time.Second)); err != nil {
		t.Fatalf("Sync t+10s: %v", err)
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("after Sync t+10s (inside %v MinPoll): List calls=%d want 1 — Syncer ignored MinPollInterval", minPoll, got)
	}

	if err := s.Sync(context.Background(), t0.Add(31*time.Second)); err != nil {
		t.Fatalf("Sync t+31s: %v", err)
	}
	if got := ad.listCalls(); got != 2 {
		t.Fatalf("after Sync t+31s (past MinPoll): List calls=%d want 2 — Syncer skipped MinPoll-due poll", got)
	}
}

// TestSyncer_HonoursMinPollInterval_FirstCallAlwaysFires pins #888 §9.2 — zero-value lastPoll fires on first Sync regardless of MinPoll.
func TestSyncer_HonoursMinPollInterval_FirstCallAlwaysFires(t *testing.T) {
	ad := &countingMinPollAdapter{minPoll: 24 * time.Hour}
	db := newSyncTestDB(t)
	s := mustNew(t, adaptersync.Config{Adapter: ad, DB: db})

	if err := s.Sync(context.Background(), time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("first Sync List calls=%d want 1 — zero-value lastPoll must not gate first call", got)
	}
}

// TestSyncer_HonoursMinPollInterval_UpdatesLastPollOnError pins #888 §9.3 — rate-limited adapter is not re-tried inside the budget window.
func TestSyncer_HonoursMinPollInterval_UpdatesLastPollOnError(t *testing.T) {
	const minPoll = 30 * time.Second
	ad := &countingMinPollAdapter{minPoll: minPoll, listErr: fmt.Errorf("synthetic rate-limit: %w", schemas.ErrRateLimited)}
	db := newSyncTestDB(t)
	s := mustNew(t, adaptersync.Config{Adapter: ad, DB: db})

	t0 := time.Unix(1_700_000_000, 0).UTC()
	if err := s.Sync(context.Background(), t0); err == nil {
		t.Fatal("Sync t0: want error from adapter, got nil")
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("after Sync t0: List calls=%d want 1", got)
	}

	// Inside MinPoll window — must NOT re-fire even though prior call errored.
	if err := s.Sync(context.Background(), t0.Add(10*time.Second)); err != nil {
		t.Fatalf("Sync t+10s: want nil short-circuit, got %v", err)
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("after Sync t+10s inside MinPoll after error: List calls=%d want 1 — lastPoll must advance on error too", got)
	}
}

// TestSyncer_HonoursMinPollInterval_ZeroMinPollAlwaysFires pins #888 §9.4 — zero MinPollInterval skips the cadence gate.
func TestSyncer_HonoursMinPollInterval_ZeroMinPollAlwaysFires(t *testing.T) {
	ad := &countingMinPollAdapter{minPoll: 0}
	db := newSyncTestDB(t)
	s := mustNew(t, adaptersync.Config{Adapter: ad, DB: db})

	t0 := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 3; i++ {
		if err := s.Sync(context.Background(), t0); err != nil {
			t.Fatalf("Sync iter %d: %v", i, err)
		}
	}
	if got := ad.listCalls(); got != 3 {
		t.Fatalf("List calls=%d want 3 — MinPoll=0 must skip the gate", got)
	}
}

// TestSyncer_AdapterPollErrorCounter pins #889 ported from scheduler: List error increments regatta.adaptersync.adapter_poll.errors_total.
func TestSyncer_AdapterPollErrorCounter(t *testing.T) {
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	ad := &countingMinPollAdapter{minPoll: 5 * time.Second, listErr: schemas.ErrRateLimited}
	db := newSyncTestDB(t)
	s := mustNew(t, adaptersync.Config{Adapter: ad, DB: db, Meter: mp.Meter("adaptersync-test")})

	if err := s.Sync(context.Background(), time.Unix(1_700_000_000, 0).UTC()); err == nil {
		t.Fatal("Sync: want error from adapter, got nil")
	}

	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var got int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "regatta.adaptersync.adapter_poll.errors_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data type=%T want metricdata.Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				got += dp.Value
			}
		}
	}
	if got != 1 {
		t.Fatalf("regatta.adaptersync.adapter_poll.errors_total = %d, want 1 — error path must increment counter", got)
	}
}

// TestSyncer_PollOnceFiresSyncerOnce pins #888 §9.6 — direct Sync calls inside one MinPoll window yield exactly one adapter.List.
func TestSyncer_PollOnceFiresSyncerOnce(t *testing.T) {
	const minPoll = 30 * time.Second
	ad := &countingMinPollAdapter{minPoll: minPoll}
	db := newSyncTestDB(t)
	s := mustNew(t, adaptersync.Config{Adapter: ad, DB: db})

	t0 := time.Unix(1_700_000_000, 0).UTC()
	// Simulate three orchestrator PollOnce ticks at PollInterval=15s; the
	// MinPoll=30s gate must collapse them to one List.
	for _, dt := range []time.Duration{0, 15 * time.Second, 29 * time.Second} {
		if err := s.Sync(context.Background(), t0.Add(dt)); err != nil {
			t.Fatalf("Sync +%v: %v", dt, err)
		}
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("three PollOnce ticks inside one MinPoll window: List calls=%d want 1 — #888 duplicate-poll regression", got)
	}
}

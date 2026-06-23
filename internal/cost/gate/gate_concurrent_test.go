package gate_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// probeEstimator counts concurrent Estimate calls so we can prove the
// Gate's mutex serialises Evaluate (R-MEGA-2 C1).
type probeEstimator struct {
	usd          float64
	inFlight     atomic.Int32
	maxInFlight  atomic.Int32
	holdDuration time.Duration
}

func (p *probeEstimator) Estimate(_ context.Context, _ gate.EstHint, _ string) (float64, error) {
	n := p.inFlight.Add(1)
	for {
		cur := p.maxInFlight.Load()
		if n <= cur || p.maxInFlight.CompareAndSwap(cur, n) {
			break
		}
	}
	time.Sleep(p.holdDuration)
	p.inFlight.Add(-1)
	return p.usd, nil
}

// TestEvaluator_ConcurrentEvaluate_RespectsCap asserts Gate.mu serialises Evaluate against TOCTOU (R-MEGA-2 C1).
func TestEvaluator_ConcurrentEvaluate_RespectsCap(t *testing.T) {
	db := statetest.OpenMigratedRaw(t)
	insertSpend(t, db, `{"usd":80.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
		time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), "default")
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 100, Period: time.Hour}}
	r := spend.NewReader(db, func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) })
	probe := &probeEstimator{usd: 5, holdDuration: 5 * time.Millisecond}
	g := gate.New(cfg, fixedPricing{}, r, probe)

	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, _ = g.Evaluate(context.Background(), baseScope())
		}()
	}
	wg.Wait()

	if got := probe.maxInFlight.Load(); got > 1 {
		t.Fatalf("maxInFlight=%d; want ≤1 (Evaluate must be serialised by Gate.mu)", got)
	}
}

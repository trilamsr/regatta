package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/gates/l4"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// BenchmarkSchedulerTick_FilterMonomorphization exercises all 3 filter.Apply[Scope] instantiation sites (closes #753).
func BenchmarkSchedulerTick_FilterMonomorphization(b *testing.B) {
	sizes := []int{10, 100}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("approval/N=%d", n), func(b *testing.B) {
			benchFilterApproval(b, n)
		})
		b.Run(fmt.Sprintf("cost/N=%d", n), func(b *testing.B) {
			benchFilterCost(b, n)
		})
		b.Run(fmt.Sprintf("l4/N=%d", n), func(b *testing.B) {
			benchFilterL4(b, n)
		})
		b.Run(fmt.Sprintf("all3/N=%d", n), func(b *testing.B) {
			benchFilterAll3(b, n)
		})
	}
}

func benchSeedPlanned(b *testing.B, db *state.DB, ids []string) {
	b.Helper()
	at := time.Unix(1_700_000_000, 0)
	for _, id := range ids {
		w := state.WorkItem{
			ID: id, Kind: state.KindFeature, Title: id,
			Lane: "server", Status: state.WorkStatusPlanned,
		}
		if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, at); err != nil {
			b.Fatalf("seed %s: %v", id, err)
		}
	}
}

func benchIDs(prefix string, n int) []string {
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("%s-%05d", prefix, i)
	}
	return ids
}

// benchPauseGate returns Pause for every gated wi so filter.Apply
// drops the wi and the reservation loop pays zero — isolates monomorphized
// closure cost from the post-filter tx path.
type benchPauseGate struct{}

func (benchPauseGate) Evaluate(_ context.Context, _ state.WorkItem, _ approval.Config) (approval.Result, error) {
	return approval.ResultPause, nil
}

type benchDenyCostGate struct{}

func (benchDenyCostGate) Evaluate(_ context.Context, _ gate.WorkItemScope) (gate.Verdict, error) {
	return gate.Verdict{Allow: false, Reason: "bench"}, nil
}

type benchBlockL4Gate struct{}

func (benchBlockL4Gate) Evaluate(_ context.Context, _ l4.Config, _ l4.Input) (schemas.GateResult, error) {
	return schemas.GateResult{Verdict: schemas.VerdictFail, Blocking: true}, nil
}

func approvalResolverAll(cfg approval.Config) GateResolver {
	return func(_ state.WorkItem) (approval.Config, bool) { return cfg, true }
}

func costResolverAll() CostGateResolver {
	return func(wi state.WorkItem) (gate.WorkItemScope, bool) {
		return gate.WorkItemScope{WorkItemID: wi.ID}, true
	}
}

func l4ResolverAll() L4GateResolver {
	return func(wi state.WorkItem) (L4Scope, bool) {
		return L4Scope{
			Cfg: l4.Config{GateID: "l4_bench"},
			In:  l4.Input{RunID: wi.ID, PRSHA: "sha-" + wi.ID},
		}, true
	}
}

func benchFilterApproval(b *testing.B, n int) {
	db := newBenchDB(b)
	benchSeedPlanned(b, db, benchIDs("A", n))
	sch := New(db, Config{
		LockTTL: time.Minute,
		Logger:  discardLogger,
		Gate:    benchPauseGate{},
		GateResolver: approvalResolverAll(approval.Config{
			Name: "bench", RiskClass: approval.RiskLow,
			Reviewers: []string{"alice"}, Quorum: 1,
			Timeout: time.Hour, DecisionWindow: time.Minute,
			OnTimeout: approval.OnTimeoutFail,
		}),
	})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sch.Tick(ctx); err != nil {
			b.Fatalf("Tick: %v", err)
		}
	}
}

func benchFilterCost(b *testing.B, n int) {
	db := newBenchDB(b)
	benchSeedPlanned(b, db, benchIDs("C", n))
	sch := New(db, Config{
		LockTTL:          time.Minute,
		Logger:           discardLogger,
		CostGate:         benchDenyCostGate{},
		CostGateResolver: costResolverAll(),
	})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sch.Tick(ctx); err != nil {
			b.Fatalf("Tick: %v", err)
		}
	}
}

func benchFilterL4(b *testing.B, n int) {
	db := newBenchDB(b)
	benchSeedPlanned(b, db, benchIDs("L", n))
	sch := New(db, Config{
		LockTTL:        time.Minute,
		Logger:         discardLogger,
		L4Gate:         benchBlockL4Gate{},
		L4GateResolver: l4ResolverAll(),
	})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sch.Tick(ctx); err != nil {
			b.Fatalf("Tick: %v", err)
		}
	}
}

func benchFilterAll3(b *testing.B, n int) {
	db := newBenchDB(b)
	benchSeedPlanned(b, db, benchIDs("X", n))
	sch := New(db, Config{
		LockTTL: time.Minute,
		Logger:  discardLogger,
		Gate:    benchPauseGate{},
		GateResolver: approvalResolverAll(approval.Config{
			Name: "bench", RiskClass: approval.RiskLow,
			Reviewers: []string{"alice"}, Quorum: 1,
			Timeout: time.Hour, DecisionWindow: time.Minute,
			OnTimeout: approval.OnTimeoutFail,
		}),
		CostGate:         benchDenyCostGate{},
		CostGateResolver: costResolverAll(),
		L4Gate:           benchBlockL4Gate{},
		L4GateResolver:   l4ResolverAll(),
	})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sch.Tick(ctx); err != nil {
			b.Fatalf("Tick: %v", err)
		}
	}
}

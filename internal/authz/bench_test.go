package authz_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/web"
)

func BenchmarkAuthorizerCheck_p99Under200Micros(b *testing.B) {
	a, err := authz.NewOPAAuthorizer(authz.Config{})
	if err != nil {
		b.Fatalf("NewOPAAuthorizer: %v", err)
	}
	if err := a.Hydrate(context.Background()); err != nil {
		b.Fatalf("Hydrate: %v", err)
	}
	p := web.Principal{ID: "alice", Tenant: "default"}
	ctx := context.Background()

	// Spec §5 R1: 200 µs steady-state, N=10 000. The first ~256 calls
	// hydrate OPA's per-query internal caches (vtree, partial-eval
	// canon), so the budget is measured AFTER warmup, not including it.
	for i := 0; i < 1024; i++ {
		if _, err := a.Check(ctx, p, authz.ActionApprovalDecide, "01HZX0000000000000000RESRC"); err != nil {
			b.Fatalf("warm Check[%d]: %v", i, err)
		}
	}

	// Spec §5 R1: p99 measured at N=10 000. Each benchmark Run executes
	// 10 000 inner iterations regardless of b.N; b.N controls how many
	// repeats the go test runner asks for. ReportMetric carries the
	// p99 observation; the budget assertion runs every Run.
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		const samples = 10_000
		lat := make([]time.Duration, 0, samples)
		b.StartTimer()
		for j := 0; j < samples; j++ {
			t0 := time.Now()
			if _, err := a.Check(ctx, p, authz.ActionApprovalDecide, "01HZX0000000000000000RESRC"); err != nil {
				b.Fatalf("Check[%d]: %v", j, err)
			}
			lat = append(lat, time.Since(t0))
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		p99 := lat[(samples*99)/100]
		b.ReportMetric(float64(p99.Microseconds()), "p99-us")
		if p99 > 200*time.Microsecond {
			b.Fatalf("p99=%v > 200µs (spec §5 R1 budget)", p99)
		}
	}
}

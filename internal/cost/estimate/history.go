// Package estimate — history.go adds the opt-in p95-of-recorded-spend
// estimator gated by `safety.cost.estimation_strategy: history`
// (spec §10 S1, issue #238). Default remains upper_bound — this type
// is constructed and wired ONLY when the operator flips the flag.
//
// Replay-safety (W9 forward-fit): the p95 is a pure function of the
// substrate snapshot the Reader sees. Sorted-then-indexed quantile (no
// random sampling, no time.Now in the estimator itself — the Reader's
// injected clock is the only time source) keeps the result deterministic
// across identical snapshots — same property the upper_bound default holds.
//
// Cold-start fallback: when fewer than MinSamples rows exist for the
// (tenant_id, model) cohort, Estimate delegates to Fallback (production
// wiring passes a gate.Estimator adapter over UpperBound). This is the
// load-bearing escape hatch — first-ever call per cohort cannot generate
// a meaningful p95, so we keep the "never-undercount" Waxell-$47K-trap
// defense intact.
package estimate

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/trilamsr/regatta/internal/cost/gate"
)

// CohortReader is the seam the History estimator consumes. *spend.Reader
// satisfies it; tests can stub without touching the substrate.
type CohortReader interface {
	CohortSpends(ctx context.Context, tenantID, operatorID, model string, period time.Duration) ([]float64, error)
}

// HistoryConfig wires a History estimator. Reader is the cohort-spend
// source. Fallback is the cold-start estimator (UpperBound behind a
// gate.Estimator adapter in production wiring). MinSamples gates
// engagement; 10 per spec §10 S1 + issue #238 acceptance. Period scopes
// the recency window; defaults to 1h matching gate.SafetyCost.period().
type HistoryConfig struct {
	Reader     CohortReader
	Fallback   gate.Estimator
	MinSamples int
	Period     time.Duration
	TenantID   string
}

// History implements gate.Estimator. Construct via NewHistory so
// defaults apply uniformly (MinSamples=10, Period=1h, TenantID="default").
type History struct {
	cfg HistoryConfig
}

// NewHistory builds a History estimator. Missing Reader or Fallback
// is a programmer error caught at wiring; the constructor panics so
// the misconfiguration surfaces at boot, not at first spawn.
func NewHistory(cfg HistoryConfig) *History {
	if cfg.Reader == nil {
		panic("estimate.NewHistory: Reader required")
	}
	if cfg.Fallback == nil {
		panic("estimate.NewHistory: Fallback required")
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 10
	}
	if cfg.Period <= 0 {
		cfg.Period = time.Hour
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "default"
	}
	return &History{cfg: cfg}
}

// Estimate returns the p95 USD over recent (tenant, operator, model)
// token_spend rows, falling back to the configured Fallback when the
// cohort holds fewer than MinSamples rows.
//
// Cohort scoping uses hint.OperatorID (populated by Gate.Evaluate from
// WorkItemScope.OperatorID). When empty (no operator pinned), the
// cohort widens to (tenant, model) — acceptable for shared-pool
// estimation but not the issue-#238 acceptance shape. Production
// wiring routes OperatorID through; tests pass it explicitly.
func (h *History) Estimate(ctx context.Context, hint gate.EstHint, model string) (float64, error) {
	samples, err := h.cfg.Reader.CohortSpends(ctx, h.cfg.TenantID, hint.OperatorID, model, h.cfg.Period)
	if err != nil {
		return 0, fmt.Errorf("estimate.History: cohort read: %w", err)
	}
	if len(samples) < h.cfg.MinSamples {
		return h.cfg.Fallback.Estimate(ctx, hint, model)
	}
	return p95(samples), nil
}

// p95 returns the nearest-rank 95th percentile. Sorted-in-place on a
// local copy so the caller's slice is not mutated.
//
// Nearest-rank: rank = ceil(0.95 × N). For N=20 → rank=19 → index 18
// of the sorted slice. Deterministic given identical input — W9 replay
// safe.
func p95(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	xs := make([]float64, len(in))
	copy(xs, in)
	sort.Float64s(xs)
	rank := int(math.Ceil(0.95 * float64(len(xs))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(xs) {
		rank = len(xs)
	}
	return xs[rank-1]
}

// Package estimate — history.go: opt-in p95-of-recorded-spend
// estimator gated by `safety.cost.estimation_strategy: history`
// (spec §10 S1, #238). Replay-safe (W9): p95 is a pure function of
// the Reader snapshot — no random sampling, no time.Now. Cold-start
// (samples < MinSamples) delegates to Fallback (UpperBound), the
// escape hatch keeping the never-undercount Waxell-$47K-trap defense
// intact.
package estimate

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/cost/spend"
)

// HistoryConfig wires a History estimator. Reader is the cohort-spend
// source (concrete *spend.Reader — Wave E inlined the prior CohortReader
// seam since only spend.Reader satisfied it). Fallback is the
// cold-start estimator. MinSamples=10 + Period=1h match spec §10 S1
// + issue #238 acceptance.
type HistoryConfig struct {
	Reader     *spend.Reader
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
// token_spend rows; falls back to Fallback when the cohort holds
// fewer than MinSamples rows. Cohort scoping uses hint.OperatorID;
// when empty the cohort widens to (tenant, model) — acceptable for
// shared-pool but not the #238 acceptance shape.
func (h *History) Estimate(ctx context.Context, hint gate.EstHint, model string) (float64, error) {
	samples, err := h.cfg.Reader.CohortSpends(ctx, h.cfg.TenantID, hint.OperatorID, model, h.cfg.Period)
	if err != nil {
		return 0, fmt.Errorf("estimate.History: cohort read: %w", err)
	}
	if len(samples) < h.cfg.MinSamples {
		return h.cfg.Fallback.Estimate(ctx, hint, model)
	}
	return p95(samples).USD(), nil
}

// p95 returns the nearest-rank 95th percentile on a local copy so the
// caller's slice is not mutated. rank = ceil(0.95 × N). Integer compare
// on int64 keeps the rank-boundary selection exact (W9 replay-safe).
func p95(in []spend.USDMicro) spend.USDMicro {
	if len(in) == 0 {
		return 0
	}
	xs := make([]spend.USDMicro, len(in))
	copy(xs, in)
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	rank := int(math.Ceil(0.95 * float64(len(xs))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(xs) {
		rank = len(xs)
	}
	return xs[rank-1]
}

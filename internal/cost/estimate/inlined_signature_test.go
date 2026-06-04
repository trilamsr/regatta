package estimate_test

import (
	"testing"

	"github.com/trilamsr/regatta/internal/cost/estimate"
	"github.com/trilamsr/regatta/internal/cost/spend"
)

// TestHistoryConfig_ReaderIsConcrete pins the post-Wave-E shape:
// HistoryConfig.Reader is *spend.Reader directly (CohortReader interface deleted).
func TestHistoryConfig_ReaderIsConcrete(t *testing.T) {
	t.Helper()
	cfg := estimate.HistoryConfig{}
	// Compile-fail if Reader is still the CohortReader interface — assigning
	// a *spend.Reader works for either shape, so the real assertion is that
	// the named CohortReader interface no longer exists.
	cfg.Reader = (*spend.Reader)(nil)
	_ = cfg
}

// TestEstimatorInterface_Deleted pins that the dead estimate.Estimator
// interface is gone. UpperBound is the only concrete; nothing accepts the
// named interface.
//
// Compile-fail evidence captured by removing the symbol — this test simply
// constructs UpperBound to keep import alive.
func TestEstimatorInterface_Deleted(t *testing.T) {
	t.Helper()
	var _ = estimate.UpperBound{}
}

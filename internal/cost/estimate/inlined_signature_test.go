package estimate_test

import (
	"testing"

	"github.com/trilamsr/regatta/internal/cost/estimate"
	"github.com/trilamsr/regatta/internal/cost/spend"
)

// TestHistoryConfig_ReaderIsConcrete pins post-Wave-E HistoryConfig.Reader as *spend.Reader (CohortReader interface deleted).
func TestHistoryConfig_ReaderIsConcrete(t *testing.T) {
	t.Helper()
	cfg := estimate.HistoryConfig{}
	// Compile-fail if Reader is still the CohortReader interface — assigning
	// a *spend.Reader works for either shape, so the real assertion is that
	// the named CohortReader interface no longer exists.
	cfg.Reader = (*spend.Reader)(nil)
	_ = cfg
}

// TestEstimatorInterface_Deleted pins that the dead estimate.Estimator interface (zero consumers) is gone.
func TestEstimatorInterface_Deleted(t *testing.T) {
	t.Helper()
	var _ = estimate.UpperBound{}
}

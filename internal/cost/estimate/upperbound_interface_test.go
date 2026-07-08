package estimate_test

import (
	"testing"

	"github.com/trilamsr/regatta/internal/cost/estimate"
	"github.com/trilamsr/regatta/internal/cost/gate"
)

// TestUpperBound_SatisfiesGateEstimator pins the R32-I6 reconcile — the gate.go:45 comment claim (#1360).
func TestUpperBound_SatisfiesGateEstimator(t *testing.T) {
	t.Helper()
	var _ gate.Estimator = estimate.UpperBound{}
}

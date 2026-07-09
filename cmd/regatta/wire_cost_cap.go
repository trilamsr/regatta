package main

import (
	"fmt"
	"log/slog"
	"time"

	costcap "github.com/trilamsr/regatta/internal/cost/cap"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// buildCostCapEnforcer wires the W5 global daily-spend ceiling into
// the scheduler; returns (nil, nil) when no `safety.cost.cap` block is
// present in regatta.yaml so scheduler.Config.CostCap stays nil and
// applyCostCap short-circuits to identity (zero overhead).
func buildCostCapEnforcer(db *state.DB, repoRoot string, clock func() time.Time, logger *slog.Logger) (scheduler.CostCapGate, error) {
	settings := loadCostCapSettingsFor(repoRoot)
	if settings.CapMicro == 0 {
		return nil, nil
	}
	tz := time.UTC
	if settings.TZ != "" {
		loc, err := time.LoadLocation(settings.TZ)
		if err != nil {
			return nil, fmt.Errorf("cost.cap.timezone %q: %w", settings.TZ, err)
		}
		tz = loc
	}
	enf, err := costcap.New(costcap.Config{
		CapMicro:   settings.CapMicro,
		TenantID:   substrate.DefaultTenantID,
		TZ:         tz,
		MemoizeTTL: settings.MemoizeTTL,
		Spend:      spend.NewReader(db.SQL(), clock),
		Recorder:   db,
		Resume:     newResumeReader(db),
		Clock:      clock,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("costcap.New: %w", err)
	}
	return enf.Allow, nil
}


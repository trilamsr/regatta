package main

import (
	"context"
	"database/sql"
	"errors"
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
// the scheduler. Returns (nil, nil) when no `safety.cost.cap` block is
// present in regatta.yaml — scheduler.Config.CostCap stays nil and
// applyCostCap short-circuits to identity (zero overhead, byte-equal
// to pre-W5 behaviour).
//
// Returning the concrete *costcap.Enforcer through the typed
// scheduler.CostCapGate interface is intentional: a nil-pointer
// wrapped in a non-nil interface would defeat the nil-check the
// scheduler relies on for the zero-overhead opt-out.
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
		Resume:     serveResumeReader{db: db},
		Clock:      clock,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("costcap.New: %w", err)
	}
	return enf, nil
}

// serveResumeReader adapts state.DB.LatestEventByKind to the
// costcap.ResumeReader interface — looks up the last cost_cap_resumed
// audit event so the Enforcer can compare it to today's day_anchor.
type serveResumeReader struct{ db *state.DB }

func (r serveResumeReader) LatestResumeAt(ctx context.Context) (time.Time, error) {
	ev, err := r.db.LatestEventByKind(ctx, costcap.EventKindResumed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return ev.CreatedAt, nil
}

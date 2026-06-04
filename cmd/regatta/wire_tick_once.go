package main

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// runTickOnce executes a single poll+schedule+route+reap cycle. The
// ordering mirrors the Run loop body so `--tick-once` is a faithful
// single-shot rehearsal of one daemon tick. Returns the wrapped first
// error so the caller can log + map to exit code 1.
func runTickOnce(ctx context.Context, o *orchestrator.Orchestrator) error {
	if err := o.PollOnce(ctx); err != nil {
		return fmt.Errorf("poll: %w", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if err := o.RouteRejections(ctx); err != nil {
		return fmt.Errorf("route rejections: %w", err)
	}
	if err := o.ReapTerminal(ctx); err != nil {
		return fmt.Errorf("reap: %w", err)
	}
	return nil
}

// Package events runs the events-table retention loop. ~17k rows/day
// (R30 heartbeat + spawn/merge/PR-watch volume) accumulates unbounded
// without a sweeper; the loop deletes rows older than the configured
// horizon every hour. RetentionDays=0 disables (R-MEGA-2 P3).
package events

import (
	"context"
	"log/slog"
	"time"
)

// Deleter is the substrate-side API the loop calls. *state.DB
// implements it via DeleteEventsOlderThan.
type Deleter interface {
	DeleteEventsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// Config wires the Retention loop. RetentionDays=0 disables the
// sweeper; Interval defaults to 1h.
type Config struct {
	DB             Deleter
	RetentionDays  int
	Interval       time.Duration
	Clock          func() time.Time
	Logger         *slog.Logger
}

// Retention runs the sweeper until Run's ctx is cancelled.
type Retention struct {
	cfg Config
}

// New constructs a Retention. Returns nil when the loop is disabled
// (DB nil or RetentionDays <= 0) so the caller can skip the goroutine.
func New(cfg Config) *Retention {
	if cfg.DB == nil || cfg.RetentionDays <= 0 {
		return nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Retention{cfg: cfg}
}

// Run blocks until ctx is cancelled. Each tick deletes events older
// than now - RetentionDays. A single failed sweep logs WARN and the
// loop continues — partial cleanup is preferable to halting forever.
func (r *Retention) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	r.sweep(ctx) // first sweep at boot so a long-uptime daemon doesn't wait Interval before any reclaim
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

func (r *Retention) sweep(ctx context.Context) {
	cutoff := r.cfg.Clock().Add(-time.Duration(r.cfg.RetentionDays) * 24 * time.Hour)
	n, err := r.cfg.DB.DeleteEventsOlderThan(ctx, cutoff)
	if err != nil {
		r.cfg.Logger.Warn("events.retention_sweep_failed",
			"cutoff", cutoff.Format(time.RFC3339),
			"err", err.Error())
		return
	}
	if n > 0 {
		r.cfg.Logger.Info("events.retention_swept",
			"deleted", n,
			"cutoff", cutoff.Format(time.RFC3339))
	}
}

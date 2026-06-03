//go:build !linux

package watchdog

import (
	"context"
	"log/slog"
	"time"
)

// Notifier is a no-op stub on non-Linux — macOS launchd has no native
// watchdog (spec §10 risk 7 → F-5 follow-up).
type Notifier struct{}

// New on non-Linux always returns nil — Run becomes a clean wait.
func New(log *slog.Logger) (*Notifier, error) { return nil, nil }

// Run blocks until ctx.Done; nothing emitted.
func (n *Notifier) Run(ctx context.Context, interval time.Duration) {
	<-ctx.Done()
}

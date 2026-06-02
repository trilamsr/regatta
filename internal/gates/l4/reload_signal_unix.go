//go:build !windows

package l4

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalLoop listens for SIGHUP and fires an immediate reload (no debounce).
// `ready` closes after signal.Notify so Run gates OnStart on a live handler
// — a SIGHUP that races the registration would otherwise hit Go's default
// disposition and terminate the test process.
func (r *Reloader) signalLoop(ctx context.Context, reloads chan<- string, ready chan<- struct{}) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	defer signal.Stop(c)
	close(ready)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c:
			select {
			case reloads <- TriggerSighup:
			case <-ctx.Done():
				return
			}
		}
	}
}

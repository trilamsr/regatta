//go:build !windows

package l4

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalLoop listens for SIGHUP and fires an immediate reload (no debounce).
func (r *Reloader) signalLoop(ctx context.Context, reloads chan<- string) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	defer signal.Stop(c)
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

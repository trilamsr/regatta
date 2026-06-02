//go:build !windows

package reload

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalLoop listens for SIGHUP and fires an immediate reload (no debounce —
// operator intent is explicit). syscall.SIGHUP is unix-only; the windows
// build replaces this file with a no-op stub.
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

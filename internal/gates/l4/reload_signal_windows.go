//go:build windows

package l4

import "context"

// signalLoop is a no-op on windows. syscall.SIGHUP is unix-only; windows
// operators rely on the fsnotify trigger or a process restart.
func (r *Reloader) signalLoop(ctx context.Context, reloads chan<- string, ready chan<- struct{}) {
	r.Logger.Info("l4 reload: SIGHUP unavailable on windows; fsnotify still active")
	close(ready)
	<-ctx.Done()
}

//go:build windows

package reload

import "context"

// signalLoop is a no-op on windows. syscall.SIGHUP is unix-only; windows
// operators rely on the fsnotify trigger or a process restart. Spec §3.6.
func (r *Reloader) signalLoop(ctx context.Context, reloads chan<- string, ready chan<- struct{}) {
	r.Logger.Info("reload: SIGHUP unavailable on windows; fsnotify still active")
	close(ready)
	<-ctx.Done()
}

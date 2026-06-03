package secrets

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// installHUPGuard installs a no-op signal.Notify so any stray SIGHUP
// that lands before Cache.Run's own Notify registers (or after it
// stops) is delivered to a Go channel instead of terminating the test
// process. Caller invokes the returned stop func via defer.
func installHUPGuard(t *testing.T) func() {
	t.Helper()
	guard := make(chan os.Signal, 8)
	signal.Notify(guard, syscall.SIGHUP)
	return func() { signal.Stop(guard) }
}

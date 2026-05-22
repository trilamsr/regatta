//go:build unix

package orchestrator

import (
	"errors"
	"syscall"
)

// osPidAlive sends signal 0 to pid. ESRCH means the process is gone;
// EPERM means it exists but is owned by another user (still alive).
// Other errors are treated as conservative-alive so a transient
// kernel issue does not mass-requeue running agents.
func osPidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}

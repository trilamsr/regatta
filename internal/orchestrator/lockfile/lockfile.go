// Package lockfile provides a process-level advisory lock keyed on a
// file path. Used by orchestrator.PollOnce to prevent concurrent
// ticks (e.g. `regatta serve` + `regatta serve --tick-once` in
// parallel) from racing the work_items + agents tables.
//
// Convention: pass `<dbPath> + ".lock"`. PollOnce derives the path
// from o.dbPath so the lockfile sits beside the sqlite file the
// operator chose.
//
// Sentinel is orchestrator.ErrFlockHeld — distinct from
// state.ErrLockHeld which is for in-process hotspot locks. Same word
// "lock", two semantic surfaces.
//
// Stale-PID reclaim: if the lockfile contains a PID that no longer
// exists (kill(pid, 0) returns ESRCH), Acquire removes the stale
// file and proceeds. Same-host assumption — pid-based liveness has
// no meaning across machines, but regatta is operator-local so this
// holds.
package lockfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/gofrs/flock"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// Lock represents a held advisory lock. Call Release when done.
type Lock struct {
	path string
	fl   *flock.Flock
}

// Acquire takes an exclusive lock on path. Returns ErrFlockHeld
// (wrapped) if another live process holds the lock. Reclaims if the
// lockfile contains a stale PID.
func Acquire(path string) (*Lock, error) {
	if err := maybeReclaimStale(path); err != nil {
		return nil, err
	}

	fl := flock.New(path)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lockfile: trylock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: %s", orchestrator.ErrFlockHeld, path)
	}

	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid), 0o600); err != nil {
		_ = fl.Unlock()
		return nil, fmt.Errorf("lockfile: write pid: %w", err)
	}
	return &Lock{path: path, fl: fl}, nil
}

// Release removes the lockfile and releases the advisory lock.
func (l *Lock) Release() error {
	if l == nil || l.fl == nil {
		return nil
	}
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("lockfile: unlock: %w", err)
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("lockfile: remove: %w", err)
	}
	return nil
}

func maybeReclaimStale(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lockfile: read: %w", err)
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return os.Remove(path)
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Garbage content — treat as stale, reclaim.
		return os.Remove(path)
	}
	if processAlive(pid) {
		return nil
	}
	return os.Remove(path)
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the process exists but we can't signal it —
	// still alive from our perspective.
	if errno, ok := err.(syscall.Errno); ok && errno == syscall.EPERM {
		return true
	}
	return false
}

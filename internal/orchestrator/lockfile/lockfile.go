// Package lockfile provides a process-level advisory lock keyed on a
// file path. Used by orchestrator.PollOnce to prevent concurrent
// ticks (e.g. `regatta serve` + `regatta serve --tick-once` in
// parallel) from racing the work_items + agents tables.
//
// Convention: pass `<dbPath> + ".lock"`. PollOnce derives the path
// from o.dbPath so the lockfile sits beside the sqlite file the
// operator chose.
//
// Design: the lockfile is a persistent `.pid` file (operator
// convention, analogous to /var/run/foo.pid). The kernel's flock()
// on the file is the lock; the file content is the holder's PID,
// written purely as a diagnostic aid (so a contender's error
// message can name the holder). Acquire never removes the file;
// Release never removes the file. This eliminates the dual-resource
// TOCTOU class where unlinking the path lets a contender create a
// new inode at the same path while the original holder still owns
// the flock on the old inode.
//
// Sentinel is orchestrator.ErrFlockHeld — distinct from
// state.ErrLockHeld which is for in-process hotspot locks. Same
// word "lock", two semantic surfaces.
//
// Same-host assumption: flock() semantics are per-host. Operators
// MUST NOT point two regatta processes on different hosts at a
// shared state.db (e.g. NFS-mounted) — flock cannot serialize them.
package lockfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gofrs/flock"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// Lock represents a held advisory lock. Call Release when done.
type Lock struct {
	path string
	fl   *flock.Flock
}

// Acquire takes an exclusive flock on path. The lockfile persists
// across releases (operator-visible `.pid` convention). The file
// content is the holder's PID, written under the flock as a
// diagnostic aid — it is NOT used for lock liveness. Liveness is
// the flock itself.
func Acquire(path string) (*Lock, error) {
	fl := flock.New(path)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lockfile: trylock: %w", err)
	}
	if !locked {
		holder := readHolderPID(path)
		return nil, fmt.Errorf("%w: %s (holder pid=%s)", orchestrator.ErrFlockHeld, path, holder)
	}

	// We hold the flock. Overwrite the PID under it so the next
	// contender sees who we are.
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid), 0o600); err != nil {
		_ = fl.Unlock()
		return nil, fmt.Errorf("lockfile: write pid: %w", err)
	}
	return &Lock{path: path, fl: fl}, nil
}

// Release unlocks the flock. The lockfile is NOT removed: it
// persists as a diagnostic .pid file. The next Acquire will
// overwrite the PID under its own flock.
func (l *Lock) Release() error {
	if l == nil || l.fl == nil {
		return nil
	}
	err := l.fl.Unlock()
	l.fl = nil
	if err != nil {
		return fmt.Errorf("lockfile: unlock: %w", err)
	}
	return nil
}

// readHolderPID returns the PID string from the lockfile (or "" on
// any error). Purely diagnostic — never used for lock liveness.
func readHolderPID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

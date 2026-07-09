// Package lockfile provides a process-level advisory lock keyed on a
// file path. orchestrator.PollOnce uses it to prevent concurrent ticks
// (e.g. `regatta serve` + `regatta serve --tick-once`) from racing the
// work_items + agents tables.
//
// Convention: pass `<dbPath> + ".lock"`. PollOnce derives the path
// from o.dbPath so the lockfile sits beside the sqlite file.
//
// Design: the kernel's flock() on a persistent `.pid` file is the
// lock; the file content is the holder's PID, written purely for
// diagnostics (so contender error messages can name the holder).
// Acquire/Release never remove the file — eliminates the dual-resource
// TOCTOU class where unlinking lets a contender create a new inode at
// the same path while the original holder still owns the flock on the
// old inode. Operators can delete the .pid file when no regatta
// process is running (verify via `lsof <path>`); regatta itself never
// removes it.
//
// Same-host only: flock() is per-host. Operators MUST NOT point two
// regatta processes on different hosts at a shared state.db (e.g.
// NFS-mounted) — flock cannot serialize them.
package lockfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrFlockHeld fires when the process-level lockfile is already held
// by another live regatta instance. Distinct from state.ErrLockHeld
// (in-process hotspot locks); same word, two semantic surfaces.
var ErrFlockHeld = errors.New("orchestrator: process flock held by another instance")

// Lock represents a held advisory lock; call Release when done.
type Lock struct {
	f *os.File
}

// Acquire takes an exclusive flock on path; the file persists across
// releases (operator-visible `.pid` convention). Content is the
// holder's PID written under the flock as a diagnostic aid — liveness
// is the flock, not the PID. Linux/darwin only — Windows is out of
// scope for regatta.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("lockfile: mkdir parent: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- caller-controlled `<dbPath>.lock`.
	if err != nil {
		return nil, fmt.Errorf("lockfile: open: %w", err)
	}
	// LOCK_EX + LOCK_NB: exclusive, non-blocking. EWOULDBLOCK signals
	// contention — treat as "held elsewhere" and surface the holder.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			holder := readHolderPID(path)
			if holder == "" {
				return nil, fmt.Errorf("%w: %s", ErrFlockHeld, path)
			}
			return nil, fmt.Errorf("%w: %s (last-known holder pid=%s; verify with `lsof %s` before killing)",
				ErrFlockHeld, path, holder, path)
		}
		return nil, fmt.Errorf("lockfile: flock: %w", err)
	}

	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid), 0o600); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("lockfile: write pid: %w", err)
	}
	return &Lock{f: f}, nil
}

// Release unlocks without removing the file; the next Acquire
// overwrites the PID under its own flock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return fmt.Errorf("lockfile: unlock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("lockfile: close: %w", closeErr)
	}
	return nil
}

// readHolderPID returns the PID string or "" on any failure. Purely diagnostic.
func readHolderPID(path string) string {
	f, err := os.Open(path) // #nosec G304 -- path is caller-controlled (`<dbPath>.lock`), not user input.
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var buf [64]byte
	n, err := io.ReadFull(io.LimitReader(f, int64(len(buf))), buf[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ""
	}
	s := strings.TrimSpace(string(buf[:n]))
	if pid, err := strconv.Atoi(s); err != nil || pid <= 0 {
		return ""
	}
	return s
}

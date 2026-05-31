package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func TestAcquire_Release_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lockfile must persist as diagnostic .pid file: %v", err)
	}
	// A new Acquire on the same path must succeed — flock released.
	lock2, err := Acquire(path)
	if err != nil {
		t.Fatalf("second Acquire after Release: %v", err)
	}
	defer lock2.Release()
}

func TestAcquire_HeldByLivePID_ReturnsErrFlockHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	_, err = Acquire(path)
	if !errors.Is(err, orchestrator.ErrFlockHeld) {
		t.Fatalf("second Acquire err=%v want ErrFlockHeld", err)
	}
}

func TestAcquire_OverwritesStalePIDContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	staleContent := []byte(strconv.Itoa(0x7FFFFFFE))
	if err := os.WriteFile(path, staleContent, 0o600); err != nil {
		t.Fatalf("plant stale lockfile: %v", err)
	}

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire over stale pid file: %v", err)
	}
	defer lock.Release()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after acquire: %v", err)
	}
	wantPID := strconv.Itoa(os.Getpid())
	if strings.TrimSpace(string(got)) != wantPID {
		t.Fatalf("lockfile content=%q want %q", got, wantPID)
	}
}

func TestRelease_IdempotentOnDoubleCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release should be no-op: %v", err)
	}
}

func TestAcquire_DualHolderImpossible_WithStalePID(t *testing.T) {
	// Regression for the dual-holder TOCTOU. The bug required a
	// stale (dead) PID present in the lockfile so the old
	// maybeReclaimStale path would os.Remove it under a live
	// flock holder, allowing a second inode at the same path.
	// New design: no remove, so the bug class is gone.
	// Two concurrent Acquires after planting a dead-PID file MUST
	// produce exactly one winner; the loser MUST get ErrFlockHeld.
	path := filepath.Join(t.TempDir(), "test.lock")
	for i := 0; i < 50; i++ {
		if err := os.WriteFile(path, []byte(strconv.Itoa(0x7FFFFFFE)), 0o600); err != nil {
			t.Fatalf("iter %d plant: %v", i, err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		var lockA, lockB *Lock
		var errA, errB error
		go func() { defer wg.Done(); lockA, errA = Acquire(path) }()
		go func() { defer wg.Done(); lockB, errB = Acquire(path) }()
		wg.Wait()

		wins := 0
		if errA == nil {
			wins++
			_ = lockA.Release()
		} else if !errors.Is(errA, orchestrator.ErrFlockHeld) {
			t.Fatalf("iter %d errA=%v want nil or ErrFlockHeld", i, errA)
		}
		if errB == nil {
			wins++
			_ = lockB.Release()
		} else if !errors.Is(errB, orchestrator.ErrFlockHeld) {
			t.Fatalf("iter %d errB=%v want nil or ErrFlockHeld", i, errB)
		}
		if wins != 1 {
			t.Fatalf("iter %d wins=%d want exactly 1", i, wins)
		}
	}
}

func TestAcquire_CreatesMissingParentDir(t *testing.T) {
	// MkdirAll the lockfile's parent so operators don't ENOENT when
	// pointing -db at a fresh path. Regression for the operator
	// footgun the adversarial reviewer flagged.
	path := filepath.Join(t.TempDir(), "sub", "deep", "test.lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire with missing parent dir: %v", err)
	}
	defer lock.Release()
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent dir not created: %v", err)
	}
}

package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lockfile not removed: %v", err)
	}
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

func TestAcquire_StalePID_Reclaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	// Plant a lockfile containing a very high PID guaranteed to be
	// unallocated (kill(pid, 0) returns ESRCH).
	staleContent := []byte(strconv.Itoa(0x7FFFFFFE))
	if err := os.WriteFile(path, staleContent, 0o600); err != nil {
		t.Fatalf("plant stale lockfile: %v", err)
	}

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after stale: %v", err)
	}
	defer lock.Release()
}

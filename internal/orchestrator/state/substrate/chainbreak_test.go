package substrate_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// newTestSweeper builds a Sweeper against a fresh temp DB with an
// injected RO handle (G2 caller-injection contract).
func newTestSweeper(t *testing.T) *substrate.Sweeper {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sweeper.db")
	ro, err := sql.Open("sqlite", substrate.ReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatalf("open RO: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	ro.SetMaxOpenConns(1)
	s, err := substrate.NewSweeper(substrate.SweeperConfig{
		RODB:    ro,
		Keyring: testKeyring(),
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}
	return s
}

// TestSweeper_CloseWithoutStart_DoesNotDeadlock proves #640 fix: Close before Start returns.
func TestSweeper_CloseWithoutStart_DoesNotDeadlock(t *testing.T) {
	s := newTestSweeper(t)
	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Sweeper.Close() deadlocked when Start() was never called")
	}
}

// TestSweeper_CloseAfterStart_StopsCleanly asserts the started-loop path still drains.
func TestSweeper_CloseAfterStart_StopsCleanly(t *testing.T) {
	s := newTestSweeper(t)
	s.Start(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Sweeper.Close() did not stop a running sweeper within 2s")
	}
}

// TestSweeper_CloseTwice_DoesNotPanic asserts Close is idempotent.
func TestSweeper_CloseTwice_DoesNotPanic(t *testing.T) {
	s := newTestSweeper(t)
	s.Start(context.Background())
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close must be a no-op — substrate.Close fans out to sweeper
	// without tracking started/closed state itself.
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Close() panicked on second call: %v", r)
			}
			close(done)
		}()
		_ = s.Close()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Close() deadlocked")
	}
}

// TestSweeper_CloseTwiceWithoutStart_DoesNotPanic covers double-close on the never-started path.
func TestSweeper_CloseTwiceWithoutStart_DoesNotPanic(t *testing.T) {
	s := newTestSweeper(t)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Close() panicked: %v", r)
			}
			close(done)
		}()
		_ = s.Close()
		_ = s.Close()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("double Close() without Start deadlocked")
	}
}

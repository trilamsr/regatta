package substrate_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// newTestDivergenceReader wires a reader against a fresh migrated DB
// with a short poll interval so tests drive the loop deterministically.
func newTestDivergenceReader(t *testing.T) *substrate.DivergenceReader {
	t.Helper()
	db := openMigratedDB(t)
	r, err := substrate.NewDivergenceReader(substrate.DivergenceReaderConfig{
		DB:           db,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDivergenceReader: %v", err)
	}
	return r
}

// TestDivergenceReader_ConcurrentStart_CloseDoesNotDeadlock proves the Start-race fix.
func TestDivergenceReader_ConcurrentStart_CloseDoesNotDeadlock(t *testing.T) {
	r := newTestDivergenceReader(t)
	ctx := context.Background()

	const starters = 4
	var wg sync.WaitGroup
	wg.Add(starters)
	for i := 0; i < starters; i++ {
		go func() {
			defer wg.Done()
			r.Start(ctx)
		}()
	}
	wg.Wait()

	done := make(chan struct{})
	go func() {
		_ = r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("DivergenceReader.Close() deadlocked after concurrent Start() calls; sync.Once guard missing")
	}
}

// TestDivergenceReader_CloseWithoutStart_DoesNotDeadlock covers the pre-Start Close path.
func TestDivergenceReader_CloseWithoutStart_DoesNotDeadlock(t *testing.T) {
	r := newTestDivergenceReader(t)
	done := make(chan struct{})
	go func() {
		_ = r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("DivergenceReader.Close() deadlocked when Start() was never called")
	}
}

// TestDivergenceReader_CloseTwice_DoesNotPanic asserts Close idempotency.
func TestDivergenceReader_CloseTwice_DoesNotPanic(t *testing.T) {
	r := newTestDivergenceReader(t)
	r.Start(context.Background())
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("Close() panicked on second call: %v", rec)
			}
			close(done)
		}()
		_ = r.Close()
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second Close() deadlocked")
	}
}

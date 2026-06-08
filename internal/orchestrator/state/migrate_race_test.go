package state

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

// TestMigrate_ConcurrentCallsRaceFree asserts state.Migrate is safe under concurrent invocation (#1020).
func TestMigrate_ConcurrentCallsRaceFree(t *testing.T) {
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			path := filepath.Join(t.TempDir(), "race.db")
			db, err := Open(context.Background(), path)
			if err != nil {
				errs <- err
				return
			}
			_ = db.Close()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Open/Migrate failed: %v", err)
		}
	}
}

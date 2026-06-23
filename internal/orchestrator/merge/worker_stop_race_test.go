package merge_test

import (
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestWorker_EnqueueAfterStop_NoPanic asserts Stop does not race Enqueue into a closed-channel panic (R-MEGA-2 C2).
func TestWorker_EnqueueAfterStop_NoPanic(t *testing.T) {
	db := statetest.OpenDB(t)
	c := newCoordinatorWithExecutor(t, db, &fakeExecutor{})
	w := merge.NewWorker(c, 4, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Stop()

	var wg sync.WaitGroup
	const N = 100
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(id int) {
			defer wg.Done()
			if ok := w.Enqueue(merge.MergeRequest{AgentID: int64(id), PRNumber: id, HeadSHA: "s"}); ok {
				t.Errorf("Enqueue returned true after Stop; want false")
			}
		}(i)
	}
	wg.Wait()
}

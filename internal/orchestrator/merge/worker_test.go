package merge_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestWorker_DrainsQueueAndExecutes asserts the worker pulls one request, runs ExecuteMerge, and drives the agent to Done.
func TestWorker_DrainsQueueAndExecutes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{Result: merge.ExecutorResult{
		Outcome: merge.OutcomeMerged, MergeSHA: "ok",
	}}
	c := newCoordinatorWithExecutor(t, db, exec)

	w := merge.NewWorker(c, 4, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); w.Run(ctx) }()

	if !w.Enqueue(merge.MergeRequest{AgentID: a.ID, PRNumber: 42, HeadSHA: "sha"}) {
		t.Fatalf("enqueue failed on empty queue")
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 10*time.Millisecond, func() bool {
		got, _ := db.GetAgent(ctx, a.ID)
		return got != nil && got.State == state.AgentDone
	}, "worker drain incomplete: agent never reached Done")
	w.Stop()
	wg.Wait()
}

// TestWorker_QueueFull_DropsAndContinues asserts a full buffer sheds new requests via Enqueue=false rather than blocking.
func TestWorker_QueueFull_DropsAndContinues(t *testing.T) {
	db := statetest.OpenDB(t)
	c := newCoordinatorWithExecutor(t, db, &fakeExecutor{})
	w := merge.NewWorker(c, 1, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// First enqueue lands in the buffer; second has no consumer yet
	// and should be shed.
	if !w.Enqueue(merge.MergeRequest{AgentID: 1, PRNumber: 1, HeadSHA: "a"}) {
		t.Fatalf("first enqueue failed")
	}
	if w.Enqueue(merge.MergeRequest{AgentID: 2, PRNumber: 2, HeadSHA: "b"}) {
		t.Fatalf("over-buffer enqueue accepted; queue-full shedding broken")
	}
}

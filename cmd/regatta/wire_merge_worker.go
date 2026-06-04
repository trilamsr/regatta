package main

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
)

// startMergeWorker spins the merge.Worker goroutine and returns a
// shutdown closure the caller defers. Nil worker (no --auto-merge)
// is a no-op so the deferred Stop+wait is byte-equal to "no merge
// worker" — PHASE-AUTONOMY §11 W2 c2 default-off contract.
func startMergeWorker(ctx context.Context, w *merge.Worker) func() {
	if w == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	return func() {
		w.Stop()
		<-done
	}
}

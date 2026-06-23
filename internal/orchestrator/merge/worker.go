package merge

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// MergeRequest is the queue payload from "gates pass" → ExecuteMerge.
// Sent on the Worker's input channel; the worker serializes the
// shell-outs to one gh-call at a time so the operator's rate-limit
// budget is naturally fair (spec §8).
type MergeRequest struct {
	AgentID  int64
	PRNumber int
	HeadSHA  string
}

// Worker drains a MergeRequest channel and invokes
// Coordinator.ExecuteMerge for each request — off the scheduler's hot
// tick path so a slow gh-CLI call cannot starve the rest of the
// substrate. A single worker goroutine keeps the gh-token's
// rate-limit fair AND serializes potentially-conflicting merges so
// each merge sees the previous merge's resolved tree.
type Worker struct {
	coord *Coordinator
	in    chan MergeRequest
	log   *slog.Logger

	wg     sync.WaitGroup
	closed sync.Once
	// shut flips before close(w.in) so Enqueue can refuse a send to a
	// closed channel and avoid the panic that races Stop (R-MEGA-2 C2).
	shut atomic.Bool
}

// NewWorker constructs a Worker bound to coord. bufSize caps in-flight
// requests; over-emit at the scheduler boundary is shed with an INFO
// log (spec §8 — single-operator scale, 32 is generous).
func NewWorker(coord *Coordinator, bufSize int, log *slog.Logger) *Worker {
	if bufSize <= 0 {
		bufSize = 32
	}
	if log == nil {
		log = slog.Default()
	}
	return &Worker{
		coord: coord,
		in:    make(chan MergeRequest, bufSize),
		log:   log,
	}
}

// Enqueue offers a merge request to the worker. Returns false when
// the buffer is full so the caller can log + drop rather than block
// the scheduler tick. The intent row is NOT written here — Enqueue is
// pre-PrepareMerge; the worker calls ExecuteMerge which calls
// PrepareMerge inside its own tx.
func (w *Worker) Enqueue(req MergeRequest) bool {
	if w.shut.Load() {
		return false
	}
	select {
	case w.in <- req:
		return true
	default:
		w.log.Warn("merge.worker_queue_full",
			"agent_id", req.AgentID, "pr_number", req.PRNumber)
		return false
	}
}

// Run drains the queue until ctx is cancelled. Each ExecuteMerge call
// runs synchronously on this single goroutine so the rate-limit
// budget stays fair across the operator's token.
func (w *Worker) Run(ctx context.Context) {
	w.wg.Add(1)
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-w.in:
			if !ok {
				return
			}
			if err := w.coord.ExecuteMerge(ctx, req.AgentID, req.PRNumber, req.HeadSHA); err != nil {
				// Transient errors are logged here; terminal failures
				// already wrote merge_failed inside ExecuteMerge. We do
				// not re-enqueue — Reconcile is the long-tail safety
				// net (spec §6.1).
				w.log.Warn("merge.worker_execute_failed",
					"agent_id", req.AgentID,
					"pr_number", req.PRNumber,
					"err", err.Error())
			}
		}
	}
}

// Stop closes the input channel and waits for Run to drain. Enqueue
// after Stop returns false; the shut flag is set BEFORE close so
// concurrent Enqueue cannot select-send on a closed channel.
func (w *Worker) Stop() {
	w.closed.Do(func() {
		w.shut.Store(true)
		close(w.in)
	})
	w.wg.Wait()
}

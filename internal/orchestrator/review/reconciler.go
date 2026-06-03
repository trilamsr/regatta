package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Reconciler is the seam between L4-scoring callers and the GH-bound
// Approver (#623). Callers push signed Verdicts onto Enqueue; the Run
// goroutine drains them serially so the Approver's GH calls cannot
// interleave per PR. A future substrate-stream subscriber will sit on
// the other side of Enqueue once the gate_verdict payload extends to
// carry PR + HeadSHA (today's payload only carries gate metadata).
//
// Why an in-process channel today: GateVerdictPayload doesn't currently
// transport PR/SHA — the substrate-bound subscriber the brief envisions
// cannot be implemented without that payload extension. The in-process
// channel is the deliverable form of the subscribe seam: L4-side code
// passes the Reconciler instead of the Approver directly.
type Reconciler struct {
	approver *Approver
	ch       chan Verdict
	log      *slog.Logger
	done     chan struct{}
	dropped  int64
	mu       sync.Mutex
}

// ReconcilerConfig wires the deps. Buffer caps in-flight verdicts;
// when full, Enqueue records a drop + returns ErrQueueFull so callers
// can elect to retry under their own backoff.
type ReconcilerConfig struct {
	Approver *Approver
	Buffer   int
	Logger   *slog.Logger
}

// ErrQueueFull signals the bounded buffer is saturated. Surfaces to
// the L4 scorer so it can drop, log, or backoff per its own policy.
var ErrQueueFull = errors.New("review: reconciler queue full")

// NewReconciler constructs a Reconciler. A nil Approver returns an
// error so wiring mistakes surface at boot, not at first verdict.
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if cfg.Approver == nil {
		return nil, fmt.Errorf("review: reconciler: approver required")
	}
	buf := cfg.Buffer
	if buf <= 0 {
		buf = 64
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Reconciler{
		approver: cfg.Approver,
		ch:       make(chan Verdict, buf),
		log:      log,
		done:     make(chan struct{}),
	}, nil
}

// Enqueue pushes a verdict onto the queue. Non-blocking: a full buffer
// surfaces ErrQueueFull rather than back-pressuring the L4 scorer.
func (r *Reconciler) Enqueue(v Verdict) error {
	select {
	case r.ch <- v:
		return nil
	default:
		r.mu.Lock()
		r.dropped++
		r.mu.Unlock()
		r.log.Warn("review.reconciler.queue_full", "pr", v.PRNumber)
		return ErrQueueFull
	}
}

// Run drains the queue until ctx is cancelled. Each verdict gets the
// Approver's Reconcile; failures log but never abort the loop —
// transient GH errors should not stall the next PR's verdict.
func (r *Reconciler) Run(ctx context.Context) {
	defer close(r.done)
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-r.ch:
			start := time.Now()
			if err := r.approver.Reconcile(ctx, v); err != nil {
				r.log.Error("review.reconciler.failed",
					"pr", v.PRNumber, "err", err, "dur_ms", time.Since(start).Milliseconds())
				continue
			}
			r.log.Info("review.reconciler.ok",
				"pr", v.PRNumber, "dur_ms", time.Since(start).Milliseconds())
		}
	}
}

// Dropped reports how many Enqueue calls hit ErrQueueFull since boot.
// Operator surface for `regatta review status` (#625) so the queue-
// saturation signal is visible without parsing log lines.
func (r *Reconciler) Dropped() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Done signals when Run returns. Callers Block on this after cancelling
// ctx so test teardown is deterministic.
func (r *Reconciler) Done() <-chan struct{} { return r.done }

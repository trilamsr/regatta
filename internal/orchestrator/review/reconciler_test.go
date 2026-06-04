package review

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/testutil"
)

// TestReconciler_DrainsQueueAndCallsApprover pins #623 — enqueued verdicts reach the Approver in order.
func TestReconciler_DrainsQueueAndCallsApprover(t *testing.T) {
	var posts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/300", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s300"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/300/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		posts.Add(1)
		_, _ = w.Write([]byte(`{"id": 1}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	a := newTestApprover(t, srv.URL)
	r, err := NewReconciler(ReconcilerConfig{Approver: a, Logger: silentLogger()})
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)

	if err := r.Enqueue(Verdict{Outcome: schemas.VerdictPass, PRNumber: 300, HeadSHA: "s300", Model: "m"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, time.Second)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 5*time.Millisecond, func() bool {
		return posts.Load() > 0
	}, "reconciler never drained Verdict into Approver")
	cancel()
	<-r.Done()
	if posts.Load() != 1 {
		t.Fatalf("posts = %d, want 1", posts.Load())
	}
}

// TestReconciler_QueueFull_RecordsDrop pins back-pressure path — full buffer returns ErrQueueFull.
func TestReconciler_QueueFull_RecordsDrop(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	a := newTestApprover(t, srv.URL)
	r, _ := NewReconciler(ReconcilerConfig{Approver: a, Buffer: 1, Logger: silentLogger()})
	// Reconciler not started — the buffer fills + the next call drops.
	_ = r.Enqueue(Verdict{PRNumber: 1})
	err := r.Enqueue(Verdict{PRNumber: 2})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	if r.Dropped() != 1 {
		t.Fatalf("Dropped() = %d, want 1", r.Dropped())
	}
}

// TestReconciler_NilApprover_Errors pins constructor input validation.
func TestReconciler_NilApprover_Errors(t *testing.T) {
	if _, err := NewReconciler(ReconcilerConfig{}); err == nil {
		t.Fatal("want error on nil approver")
	}
}

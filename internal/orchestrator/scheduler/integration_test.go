//go:build integration_gh

package scheduler

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter/githubissues"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil"
)

// TestSchedulerMinPollWithRealAdapter pins MinPoll cadence under real gh latency: 6 polls hold the floor, errors don't collapse cadence, ctx cancel stops the loop (#898).
func TestSchedulerMinPollWithRealAdapter(t *testing.T) {
	const (
		minPoll       = 5 * time.Second
		tickInterval  = 500 * time.Millisecond
		wantPolls     = 6
		errInjectCall = 3
	)

	slug := os.Getenv("REGATTA_GH_FIXTURE_REPO")
	if slug == "" {
		t.Skip("REGATTA_GH_FIXTURE_REPO unset; skipping scheduler MinPoll integration test")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skipf("gh not on PATH: %v", err)
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" {
		t.Fatalf("REGATTA_GH_FIXTURE_REPO=%q is not owner/name", slug)
	}

	db, err := state.OpenWithClock(context.Background(), state.DSN(filepath.Join(t.TempDir(), "minpoll_integ.db")), time.Now)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	realAdapter, err := githubissues.NewGitHubIssues(githubissues.GitHubIssuesConfig{
		Client:  ghclient.NewGHCLIClient(owner, name),
		Repo:    githubissues.Repo{Owner: owner, Name: name},
		MinPoll: minPoll,
	})
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}

	ad := &observingAdapter{inner: realAdapter, errInjectAtCall: errInjectCall, injectErr: schemas.ErrTransient}

	sch := New(db, Config{Adapters: []schemas.SpecAdapter{ad}})

	deadline := time.Duration(wantPolls+2) * minPoll
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, terr := sch.Tick(ctx); terr != nil && !errors.Is(terr, context.Canceled) {
					t.Errorf("Tick: %v", terr)
					return
				}
			}
		}
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, deadline)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 250*time.Millisecond, func() bool {
		return ad.calls() >= wantPolls
	}, "observed >= wantPolls adapter.List calls")

	stamps := ad.stampsCopy()
	if len(stamps) < wantPolls {
		t.Fatalf("len(stamps)=%d; want >=%d", len(stamps), wantPolls)
	}
	// MinPoll contract: every inter-poll delta >= minPoll. Slack absorbs
	// the gate-to-stamp interval — scheduler.pollAdaptersHonouringMinPoll
	// gates on now()-lastPoll then this adapter's List() takes the stamp,
	// so observed delta is bounded below by minPoll minus that tiny gap.
	const slack = 50 * time.Millisecond
	for i := 1; i < len(stamps); i++ {
		delta := stamps[i].Sub(stamps[i-1])
		if delta+slack < minPoll {
			t.Errorf("inter-poll delta[%d]=%v < MinPoll=%v (slack=%v): MinPoll floor violated", i, delta, minPoll, slack)
		}
		// Soft jitter ceiling: gh-latency spikes against the real fixture
		// log a warning rather than failing the nightly job.
		upper := minPoll + tickInterval + 2*time.Second
		if delta > upper {
			t.Logf("WARN: inter-poll delta[%d]=%v exceeds soft ceiling %v (MinPoll=%v) — gh latency spike?", i, delta, upper, minPoll)
		}
	}

	if !ad.sawInjectedErr() {
		t.Logf("WARN: forced-error window never fired (calls=%d); error-resilience leg untested this run", ad.calls())
	}

	// Shutdown: cancelling ctx must stop the tick loop within a short
	// deadline so orchestrator shutdown is not wedged on a pending wait.
	cancel()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("tick loop did not exit within 2s of ctx cancel")
	}
}

// observingAdapter wraps a real SpecAdapter, stamps every List call with wall-clock time, and self-injects an error on the Nth call so cadence-after-error is exercised within one run.
type observingAdapter struct {
	inner           schemas.SpecAdapter
	errInjectAtCall int
	injectErr       error

	mu     sync.Mutex
	stamps []time.Time

	hadInjected atomic.Bool
}

// List stamps the call, returns the injected error on the configured call index, otherwise delegates to the real adapter.
func (o *observingAdapter) List(ctx context.Context) ([]schemas.WorkItem, error) {
	o.mu.Lock()
	o.stamps = append(o.stamps, time.Now())
	n := len(o.stamps)
	o.mu.Unlock()
	if o.errInjectAtCall > 0 && n == o.errInjectAtCall && o.injectErr != nil {
		o.hadInjected.Store(true)
		return nil, o.injectErr
	}
	return o.inner.List(ctx)
}

func (o *observingAdapter) Get(ctx context.Context, id schemas.WorkItemID) (schemas.WorkItem, error) {
	return o.inner.Get(ctx, id)
}

func (o *observingAdapter) UpdateStatus(ctx context.Context, id schemas.WorkItemID, st schemas.Status, citation string) error {
	return o.inner.UpdateStatus(ctx, id, st, citation)
}

func (o *observingAdapter) Capabilities() schemas.Capabilities { return o.inner.Capabilities() }

func (o *observingAdapter) calls() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.stamps)
}

func (o *observingAdapter) stampsCopy() []time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]time.Time, len(o.stamps))
	copy(out, o.stamps)
	return out
}

func (o *observingAdapter) sawInjectedErr() bool { return o.hadInjected.Load() }

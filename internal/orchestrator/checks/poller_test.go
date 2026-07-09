package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeGH scripts the GHShell Runner with per-PR CheckRun rows so the
// Poller can be exercised without a live gh binary.
type fakeGH struct {
	mu   sync.Mutex
	byPR map[string]CheckRun
	err  error
}

func (f *fakeGH) shell() *GHShell {
	return &GHShell{Runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.err != nil {
			return nil, f.err
		}
		// args: pr checks <pr> --json conclusion,status,name --required
		if len(args) < 3 {
			return nil, fmt.Errorf("unexpected gh args: %v", args)
		}
		pr := args[2]
		cr := f.byPR[pr]
		row := struct {
			Conclusion string `json:"conclusion"`
			Status     string `json:"status"`
			Name       string `json:"name"`
		}{Conclusion: cr.Conclusion, Status: cr.Status, Name: "req"}
		// Emit as an array so PRChecks JSON decode succeeds; empty
		// conclusion yields a pending row so the aggregator returns
		// the pending shape rather than success-completed.
		if cr.Conclusion == "" && cr.Status == "" {
			return []byte(`[]`), nil
		}
		return json.Marshal([]any{row})
	}}
}

func (f *fakeGH) set(pr string, cr CheckRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byPR == nil {
		f.byPR = map[string]CheckRun{}
	}
	f.byPR[pr] = cr
}

type captureSink struct {
	mu   sync.Mutex
	rows []Emission
}

func (c *captureSink) Emit(_ context.Context, e Emission) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows = append(c.rows, e)
	return nil
}

func (c *captureSink) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rows)
}

// TestPoller_FirstObservationEmits asserts the first poll against a PR emits one event regardless of prior state (#operator-console-S0).
func TestPoller_FirstObservationEmits(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})
	sink := &captureSink{}
	p := New(gh.shell(), sink)

	if err := p.Poll(context.Background(), "PR-1"); err != nil {
		t.Fatal(err)
	}
	if got := sink.count(); got != 1 {
		t.Errorf("first-observation emit count: got %d want 1", got)
	}
}

// TestPoller_FlipEmitsAgain asserts a conclusion change yields a second emission (#operator-console-S0).
func TestPoller_FlipEmitsAgain(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})
	sink := &captureSink{}
	p := New(gh.shell(), sink)

	if err := p.Poll(context.Background(), "PR-1"); err != nil {
		t.Fatal(err)
	}
	gh.set("PR-1", CheckRun{Conclusion: "failure", Status: "completed"})
	if err := p.Poll(context.Background(), "PR-1"); err != nil {
		t.Fatal(err)
	}
	if got := sink.count(); got != 2 {
		t.Errorf("after flip, expected 2 emissions; got %d", got)
	}
}

// TestPoller_NonFlipDoesNotEmit asserts identical-state polls do not re-emit (#operator-console-S0).
func TestPoller_NonFlipDoesNotEmit(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})
	sink := &captureSink{}
	p := New(gh.shell(), sink)

	for i := 0; i < 3; i++ {
		if err := p.Poll(context.Background(), "PR-1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := sink.count(); got != 1 {
		t.Errorf("3 polls same state: got %d events want 1", got)
	}
}

// TestPoller_ConcurrentPollsSafe asserts concurrent polls do not race on internal state (run -race) (#operator-console-S0).
func TestPoller_ConcurrentPollsSafe(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})
	sink := &captureSink{}
	p := New(gh.shell(), sink)

	var wg sync.WaitGroup
	var errs atomic.Int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Poll(context.Background(), "PR-1"); err != nil {
				errs.Add(1)
			}
		}()
	}
	wg.Wait()
	if errs.Load() != 0 {
		t.Errorf("got %d errors from concurrent polls", errs.Load())
	}
}

// TestPoller_GHErrorBubbles asserts an underlying gh failure surfaces to the caller without an emit (#operator-console-S0).
func TestPoller_GHErrorBubbles(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{err: errors.New("network down")}
	sink := &captureSink{}
	p := New(gh.shell(), sink)

	if err := p.Poll(context.Background(), "PR-1"); err == nil {
		t.Error("expected error, got nil")
	}
	if got := sink.count(); got != 0 {
		t.Errorf("emit on gh error: got %d want 0", got)
	}
}

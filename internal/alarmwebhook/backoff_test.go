package alarmwebhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRetryClock is a deterministic clock+sleep seam for backoff tests.
type fakeRetryClock struct {
	now    time.Time
	slept  []time.Duration
	total  time.Duration
	cancel bool
}

func (c *fakeRetryClock) Now() time.Time { return c.now }
func (c *fakeRetryClock) Sleep(ctx context.Context, d time.Duration) error {
	if c.cancel {
		return context.Canceled
	}
	c.slept = append(c.slept, d)
	c.total += d
	c.now = c.now.Add(d)
	return nil
}

// mkResp builds a *http.Response with status + header. body is a no-op
// io.NopCloser so callers can Close() it to satisfy bodyclose lint.
func mkResp(status int, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

// closeBody is a test helper that closes resp.Body if non-nil; satisfies
// bodyclose lint without burying the assertion in each test.
func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// TestBackoff_429WithRateLimitReset_SleepsUntilReset asserts the retry waits until X-RateLimit-Reset on a 429.
func TestBackoff_429WithRateLimitReset_SleepsUntilReset(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	resetAt := clock.now.Add(7 * time.Second).Unix()
	h := http.Header{}
	h.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		if calls < 2 {
			return mkResp(429, h), nil
		}
		return mkResp(200, nil), nil
	}

	r := newRetrier(retryOpts{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Jitter:      0, // deterministic
		Clock:       clock,
	})
	resp, err := r.Do(context.Background(), fn)
	defer closeBody(resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if calls != 2 {
		t.Fatalf("calls: got %d want 2", calls)
	}
	if len(clock.slept) != 1 {
		t.Fatalf("sleeps: got %d want 1", len(clock.slept))
	}
	// Expected sleep is ~7s (resetAt - now). Without jitter it must be exactly 7s.
	if clock.slept[0] != 7*time.Second {
		t.Fatalf("sleep duration: got %s want 7s", clock.slept[0])
	}
}

// TestBackoff_429NoHeader_ExponentialDelay asserts geometric backoff when X-RateLimit-Reset is absent.
func TestBackoff_429NoHeader_ExponentialDelay(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		if calls < 4 {
			return mkResp(429, nil), nil
		}
		return mkResp(200, nil), nil
	}

	r := newRetrier(retryOpts{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Jitter:      0,
		Clock:       clock,
	})
	resp, err := r.Do(context.Background(), fn)
	defer closeBody(resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if len(clock.slept) != 3 {
		t.Fatalf("sleeps: got %d want 3 (3 retries before success on call 4)", len(clock.slept))
	}
	// Base 100ms, expect 100ms, 200ms, 400ms (attempts 1,2,3).
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	for i, w := range want {
		if clock.slept[i] != w {
			t.Errorf("sleep[%d]: got %s want %s", i, clock.slept[i], w)
		}
	}
}

// TestBackoff_429ResetClampedToMaxDelay asserts a far-future X-RateLimit-Reset is clamped to MaxDelay so a hostile/forged value cannot stall retries.
func TestBackoff_429ResetClampedToMaxDelay(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	resetAt := clock.now.Add(2 * time.Hour).Unix() // wildly far future
	h := http.Header{}
	h.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		if calls < 2 {
			return mkResp(429, h), nil
		}
		return mkResp(200, nil), nil
	}
	r := newRetrier(retryOpts{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Clock:       clock,
	})
	resp, err := r.Do(context.Background(), fn)
	defer closeBody(resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if clock.slept[0] != 30*time.Second {
		t.Fatalf("sleep got %s want 30s (MaxDelay clamp)", clock.slept[0])
	}
}

// TestBackoff_MaxAttempts_ReturnsLastResponse asserts the retrier surfaces the final 429 once MaxAttempts is exhausted.
func TestBackoff_MaxAttempts_ReturnsLastResponse(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return mkResp(429, nil), nil
	}
	r := newRetrier(retryOpts{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Jitter:      0,
		Clock:       clock,
	})
	resp, err := r.Do(context.Background(), fn)
	defer closeBody(resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.StatusCode != 429 {
		t.Fatalf("final status: got %d want 429", resp.StatusCode)
	}
	if calls != 3 {
		t.Fatalf("calls: got %d want 3", calls)
	}
}

// TestBackoff_NonRetryableStatus_NoRetry asserts a 200 / 4xx (not 429) returns immediately.
func TestBackoff_NonRetryableStatus_NoRetry(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return mkResp(404, nil), nil
	}
	r := newRetrier(retryOpts{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Clock:       clock,
	})
	resp, err := r.Do(context.Background(), fn)
	defer closeBody(resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("calls: got %d want 1", calls)
	}
}

// TestBackoff_CircuitOpen_FailsFast asserts repeated 429s open the circuit and the next call returns ErrCircuitOpen without invoking fn.
func TestBackoff_CircuitOpen_FailsFast(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	r := newRetrier(retryOpts{
		MaxAttempts:        1, // each Do is a single attempt — count 429 toward circuit
		BaseDelay:          10 * time.Millisecond,
		MaxDelay:           1 * time.Second,
		Clock:              clock,
		CircuitThreshold:   10,
		CircuitWindow:      1 * time.Minute,
		CircuitOpenFor:     5 * time.Minute,
	})
	fn := func() (*http.Response, error) { return mkResp(429, nil), nil }
	// 10 consecutive 429 attempts → circuit opens.
	for i := 0; i < 10; i++ {
		resp, err := r.Do(context.Background(), fn)
		closeBody(resp)
		if err != nil {
			t.Fatalf("priming %d: %v", i, err)
		}
	}
	// 11th call must fail fast.
	callsBefore := 0
	probe := func() (*http.Response, error) {
		callsBefore++
		return mkResp(200, nil), nil
	}
	resp, err := r.Do(context.Background(), probe)
	closeBody(resp)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("11th call err: got %v want ErrCircuitOpen", err)
	}
	if callsBefore != 0 {
		t.Fatalf("probe should not be invoked while circuit open; got %d calls", callsBefore)
	}
}

// TestBackoff_CircuitCloses_AfterTimeout asserts the circuit reopens once CircuitOpenFor elapses.
func TestBackoff_CircuitCloses_AfterTimeout(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	r := newRetrier(retryOpts{
		MaxAttempts:      1,
		BaseDelay:        10 * time.Millisecond,
		MaxDelay:         1 * time.Second,
		Clock:            clock,
		CircuitThreshold: 10,
		CircuitWindow:    1 * time.Minute,
		CircuitOpenFor:   5 * time.Minute,
	})
	fn := func() (*http.Response, error) { return mkResp(429, nil), nil }
	for i := 0; i < 10; i++ {
		resp, _ := r.Do(context.Background(), fn)
		closeBody(resp)
	}
	// Sanity: circuit is now open.
	resp, err := r.Do(context.Background(), fn)
	closeBody(resp)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("pre-advance: expected ErrCircuitOpen, got %v", err)
	}
	// Advance past CircuitOpenFor → circuit closes.
	clock.now = clock.now.Add(6 * time.Minute)
	probeCalls := 0
	probe := func() (*http.Response, error) {
		probeCalls++
		return mkResp(200, nil), nil
	}
	resp, err = r.Do(context.Background(), probe)
	defer closeBody(resp)
	if err != nil {
		t.Fatalf("post-timeout err: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("post-timeout status: got %d want 200", resp.StatusCode)
	}
	if probeCalls != 1 {
		t.Fatalf("probe should run once after circuit closes; got %d", probeCalls)
	}
}

// TestBackoff_429Then200_ClosesCircuitCounter asserts a successful response clears the consecutive-429 counter.
func TestBackoff_429Then200_ClosesCircuitCounter(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0)}
	r := newRetrier(retryOpts{
		MaxAttempts:      1,
		BaseDelay:        10 * time.Millisecond,
		MaxDelay:         1 * time.Second,
		Clock:            clock,
		CircuitThreshold: 10,
		CircuitWindow:    1 * time.Minute,
		CircuitOpenFor:   5 * time.Minute,
	})
	// 9 × 429 (just below threshold).
	for i := 0; i < 9; i++ {
		resp, _ := r.Do(context.Background(), func() (*http.Response, error) { return mkResp(429, nil), nil })
		closeBody(resp)
	}
	// 1 × 200 → counter resets.
	resp, _ := r.Do(context.Background(), func() (*http.Response, error) { return mkResp(200, nil), nil })
	closeBody(resp)
	// 9 more × 429 must NOT open circuit because counter reset.
	for i := 0; i < 9; i++ {
		resp, err := r.Do(context.Background(), func() (*http.Response, error) { return mkResp(429, nil), nil })
		closeBody(resp)
		if err != nil {
			t.Fatalf("iter %d: unexpected err %v", i, err)
		}
		if resp.StatusCode != 429 {
			t.Fatalf("iter %d: status %d", i, resp.StatusCode)
		}
	}
}

// TestBackoff_ContextCancel_Aborts asserts a cancelled context returns immediately without further retries.
func TestBackoff_ContextCancel_Aborts(t *testing.T) {
	clock := &fakeRetryClock{now: time.Unix(1_700_000_000, 0), cancel: true}
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return mkResp(429, nil), nil
	}
	r := newRetrier(retryOpts{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Clock:       clock,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := r.Do(ctx, fn)
	closeBody(resp)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err: got %v want context.Canceled", err)
	}
}

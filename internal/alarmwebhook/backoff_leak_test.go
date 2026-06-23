package alarmwebhook

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingBody wraps a body reader so the test can detect Close after-the-fact.
type countingBody struct {
	io.Reader
	closes *atomic.Int64
}

func (b *countingBody) Close() error {
	b.closes.Add(1)
	return nil
}

// TestBackoff_BodyClosedOnCtxCancel asserts a 429 response retained across a ctx-cancel Sleep is drained + closed before Do returns (R-MEGA-3 LIVE-13).
func TestBackoff_BodyClosedOnCtxCancel(t *testing.T) {
	closes := &atomic.Int64{}
	clock := &fakeRetryClock{cancel: true}
	r := newRetrier(retryOpts{
		MaxAttempts:      3,
		BaseDelay:        10 * time.Millisecond,
		MaxDelay:         100 * time.Millisecond,
		CircuitThreshold: 100,
		CircuitWindow:    time.Minute,
		CircuitOpenFor:   time.Second,
		Clock:            clock,
	})
	fn := func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
			Body:       &countingBody{Reader: strings.NewReader("rate limit"), closes: closes},
		}, nil
	}
	resp, err := r.Do(context.Background(), fn)
	if resp != nil {
		_ = resp.Body.Close()
		t.Errorf("expected nil resp on ctx-cancel; got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected ctx-cancel err")
	}
	if closes.Load() == 0 {
		t.Errorf("expected lastResp body closed on ctx-cancel; closes=%d", closes.Load())
	}
}

// TestBackoff_BodyClosedAcross429Retries asserts each retained 429 response body is closed before being replaced (R-MEGA-3 LIVE-13).
func TestBackoff_BodyClosedAcross429Retries(t *testing.T) {
	closes := &atomic.Int64{}
	r := newRetrier(retryOpts{
		MaxAttempts:      3,
		BaseDelay:        time.Millisecond,
		MaxDelay:         10 * time.Millisecond,
		CircuitThreshold: 100,
		CircuitWindow:    time.Minute,
		CircuitOpenFor:   time.Second,
		Clock:            &fakeRetryClock{},
	})
	attempts := 0
	fn := func() (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
			Body:       &countingBody{Reader: strings.NewReader("429"), closes: closes},
		}, nil
	}
	resp, err := r.Do(context.Background(), fn)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	// Final (last) 429 body must be reachable for the caller to read; only
	// the earlier-retry bodies were closed.
	if resp == nil || resp.Body == nil {
		t.Fatal("expected final resp + body returned for caller")
	}
	_ = resp.Body.Close()
	// closes should be at least attempts-1 (every replaced 429 body closed).
	if closes.Load() < int64(attempts-1) {
		t.Errorf("expected ≥%d intermediate closes; got %d", attempts-1, closes.Load())
	}
}

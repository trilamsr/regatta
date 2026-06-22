package alarmwebhook

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ErrCircuitOpen signals the retrier is fail-fast: too many consecutive 429s
// observed in CircuitWindow, so callers must back off without burning another
// GH API quota slot until CircuitOpenFor elapses.
var ErrCircuitOpen = errors.New("alarmwebhook: circuit open (consecutive 429s)")

// retryClock is the time+sleep seam so tests advance backoff deterministically
// without real wall-clock waits.
type retryClock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// realClock is the production retryClock; uses time.Now + a context-aware
// sleep so a cancelled webhook drops its retry queue immediately.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryOpts configures one retrier. Defaults are baked in to keep call-sites
// terse — only MaxAttempts is load-bearing for behavior.
type retryOpts struct {
	MaxAttempts      int
	BaseDelay        time.Duration
	MaxDelay         time.Duration
	Jitter           float64 // 0..1 fractional jitter; 0 = deterministic
	Clock            retryClock
	CircuitThreshold int           // consecutive 429s before circuit opens
	CircuitWindow    time.Duration // observation window for consecutive counting
	CircuitOpenFor   time.Duration // how long the open circuit stays open
	Rand             *rand.Rand    // optional; tests inject; nil = global
}

// retrier wraps an HTTP call with 429-aware exponential backoff and a
// consecutive-429 circuit breaker. The breaker isolates GH-side quota
// exhaustion from the rest of the receiver: once open, ListOpenIssuesByLabel
// returns ErrCircuitOpen immediately so the handler can surface a 502 and
// let AlertManager retry without burning more API budget.
type retrier struct {
	opts retryOpts

	mu              sync.Mutex
	consecutive429s int
	circuitOpenedAt time.Time
}

func newRetrier(opts retryOpts) *retrier {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 5
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 500 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 30 * time.Second
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.CircuitThreshold <= 0 {
		opts.CircuitThreshold = 10
	}
	if opts.CircuitWindow <= 0 {
		opts.CircuitWindow = 1 * time.Minute
	}
	if opts.CircuitOpenFor <= 0 {
		opts.CircuitOpenFor = 5 * time.Minute
	}
	return &retrier{opts: opts}
}

// Do invokes fn, retrying on HTTP 429 until either a non-429 lands, the
// attempt budget exhausts, or the circuit breaker trips. Non-429 errors
// (transport failures, non-retryable status codes) return immediately so
// callers see them without backoff overhead.
func (r *retrier) Do(ctx context.Context, fn func() (*http.Response, error)) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.circuitOpen() {
		return nil, ErrCircuitOpen
	}
	var lastResp *http.Response
	for attempt := 0; attempt < r.opts.MaxAttempts; attempt++ {
		resp, err := fn()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			r.recordSuccess()
			return resp, nil
		}
		r.record429()
		lastResp = resp
		if attempt == r.opts.MaxAttempts-1 {
			break
		}
		delay := r.computeDelay(attempt, resp.Header)
		if err := r.opts.Clock.Sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
	return lastResp, nil
}

// computeDelay picks the next sleep duration. X-RateLimit-Reset wins when
// present and in the future; otherwise exponential backoff (BaseDelay << attempt)
// capped at MaxDelay. Jitter prevents a fleet of receivers from re-synchronising
// their retry waves after a shared GH outage.
func (r *retrier) computeDelay(attempt int, header http.Header) time.Duration {
	if v := header.Get("X-RateLimit-Reset"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			resetAt := time.Unix(ts, 0) // allow-bare-time-unix: resetAt.Sub() is duration-only; Location irrelevant.
			if d := resetAt.Sub(r.opts.Clock.Now()); d > 0 {
				// Clamp to MaxDelay so a hostile or buggy upstream cannot
				// pin the receiver waiting hours for a forged "reset"
				// timestamp far in the future.
				if d > r.opts.MaxDelay {
					d = r.opts.MaxDelay
				}
				return r.applyJitter(d)
			}
		}
	}
	// Exponential: BaseDelay * 2^attempt, capped at MaxDelay.
	d := r.opts.BaseDelay << attempt
	if d <= 0 || d > r.opts.MaxDelay {
		d = r.opts.MaxDelay
	}
	return r.applyJitter(d)
}

func (r *retrier) applyJitter(d time.Duration) time.Duration {
	if r.opts.Jitter <= 0 {
		return d
	}
	var f float64
	if r.opts.Rand != nil {
		f = r.opts.Rand.Float64()
	} else {
		// crypto-quality not needed; this only spreads retry waves.
		f = rand.Float64() //nolint:gosec
	}
	// Symmetric jitter: scale d by (1 + Jitter*(2f-1)).
	scale := 1 + r.opts.Jitter*(2*f-1)
	return time.Duration(float64(d) * scale)
}

// record429 increments the consecutive counter and opens the circuit once
// the threshold trips. Counter only resets on a non-429 response or after
// CircuitOpenFor elapses — a partial recovery (200 then 429) keeps the
// previous 429 streak from poisoning future requests.
func (r *retrier) record429() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consecutive429s++
	if r.consecutive429s >= r.opts.CircuitThreshold && r.circuitOpenedAt.IsZero() {
		r.circuitOpenedAt = r.opts.Clock.Now()
	}
}

func (r *retrier) recordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consecutive429s = 0
	r.circuitOpenedAt = time.Time{}
}

func (r *retrier) circuitOpen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.circuitOpenedAt.IsZero() {
		return false
	}
	if r.opts.Clock.Now().Sub(r.circuitOpenedAt) >= r.opts.CircuitOpenFor {
		// Timeout elapsed: reset state and let the next call probe.
		r.circuitOpenedAt = time.Time{}
		r.consecutive429s = 0
		return false
	}
	return true
}

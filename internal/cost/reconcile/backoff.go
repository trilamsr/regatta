package reconcile

import "time"

// Backoff is the exponential-backoff state machine spacing retry
// attempts against Anthropic upstream. Spec §3.4 line 247: 1s × 2^n
// capped 5min. base + max are injected so tests stay deterministic
// without time.Sleep.
type Backoff struct {
	base    time.Duration
	max     time.Duration
	attempt int
}

// NewBackoff applies spec defaults (1s base, 5min cap) when callers
// pass zero — explicit injection wins.
func NewBackoff(base, capDelay time.Duration) *Backoff {
	if base <= 0 {
		base = time.Second
	}
	if capDelay <= 0 {
		capDelay = 5 * time.Minute
	}
	return &Backoff{base: base, max: capDelay}
}

// Next returns the delay for the current attempt then advances. First
// call returns base; second returns base*2; capped at max.
func (b *Backoff) Next() time.Duration {
	d := b.delayFor(b.attempt)
	b.attempt++
	return d
}

// NextWithRetryAfter returns max(retryAfter, Next()) — honour AT LEAST
// the server's request when set (R3 + A3); fall through to plain
// exponential when zero.
func (b *Backoff) NextWithRetryAfter(retryAfter time.Duration) time.Duration {
	exp := b.Next()
	if retryAfter > exp {
		return retryAfter
	}
	return exp
}

// Reset rewinds attempt to zero so transient blips do not permanently
// inflate the wait after a successful request.
func (b *Backoff) Reset() {
	b.attempt = 0
}

func (b *Backoff) delayFor(attempt int) time.Duration {
	// Cap the shift so 1<<n cannot overflow before the duration
	// product — 62 already eclipses any reasonable max.
	const maxShift = 62
	if attempt > maxShift {
		return b.max
	}
	d := b.base << attempt
	if d <= 0 || d > b.max {
		return b.max
	}
	return d
}

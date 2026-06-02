package reconcile

import "time"

// Backoff is an exponential-backoff state machine used by the
// reconciler to space retry attempts against Anthropic upstream.
//
// Spec §3.4 line 247 pins the formula: "Exponential backoff
// (1s × 2^n, capped 5min)". The base + max are injected so tests
// stay deterministic without time.Sleep.
//
// NextWithRetryAfter honours the server's retry-after header per R3
// + A3: when the server tells us to wait longer than our local
// backoff would, we obey. The local backoff floor still applies so
// a missing/zero header falls through to pure exponential.
type Backoff struct {
	base    time.Duration
	max     time.Duration
	attempt int
}

// NewBackoff constructs a Backoff with the given base + cap delays.
// base==0 is treated as 1s; capDelay==0 as 5min — these are the spec
// defaults but explicit injection wins.
func NewBackoff(base, capDelay time.Duration) *Backoff {
	if base <= 0 {
		base = time.Second
	}
	if capDelay <= 0 {
		capDelay = 5 * time.Minute
	}
	return &Backoff{base: base, max: capDelay}
}

// Next returns the delay for the current attempt, then advances.
// The first call returns `base`; the second returns `base*2`, etc.,
// capped at `max`.
func (b *Backoff) Next() time.Duration {
	d := b.delayFor(b.attempt)
	b.attempt++
	return d
}

// NextWithRetryAfter returns max(retryAfter, Next()). When retryAfter
// is zero we fall through to plain exponential. When non-zero we
// always honour AT LEAST the server's request — pins R3 + A3.
func (b *Backoff) NextWithRetryAfter(retryAfter time.Duration) time.Duration {
	exp := b.Next()
	if retryAfter > exp {
		return retryAfter
	}
	return exp
}

// Reset rewinds attempt to zero so the next Next() returns `base`.
// Called after a successful request so transient blips do not
// permanently inflate the wait.
func (b *Backoff) Reset() {
	b.attempt = 0
}

func (b *Backoff) delayFor(attempt int) time.Duration {
	// Cap the shift so 1<<n cannot overflow before the time.Duration
	// product. 62 is way more than enough; 1s<<32 already eclipses
	// any reasonable max.
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

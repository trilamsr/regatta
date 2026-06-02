package reconcile

import (
	"testing"
	"time"
)

// TestBackoff_ExponentialBaseAndCap pins spec §3.4 line 247 — 1s × 2^n capped 5min.
func TestBackoff_ExponentialBaseAndCap(t *testing.T) {
	b := NewBackoff(time.Second, 5*time.Minute)
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		64 * time.Second,
		128 * time.Second,
		256 * time.Second,
		5 * time.Minute, // capped at 5min (would be 512s > 300s)
		5 * time.Minute, // still capped
		5 * time.Minute,
	}
	for i, w := range want {
		got := b.Next()
		if got != w {
			t.Errorf("attempt %d: got=%v want=%v", i, got, w)
		}
	}
}

// TestBackoff_ResetClearsAttempt — after Reset the state returns to attempt 0.
func TestBackoff_ResetClearsAttempt(t *testing.T) {
	b := NewBackoff(time.Second, 5*time.Minute)
	_ = b.Next()
	_ = b.Next()
	_ = b.Next() // attempt advanced to 3
	b.Reset()
	got := b.Next()
	if got != time.Second {
		t.Fatalf("after Reset Next=%v want %v", got, time.Second)
	}
}

// TestBackoff_RetryAfterHonoured pins R3 + A3 — NextWithRetryAfter returns max(retryAfter, exp).
func TestBackoff_RetryAfterHonoured(t *testing.T) {
	b := NewBackoff(time.Second, 5*time.Minute)
	// First attempt: exp=1s, retryAfter=12s → expect 12s.
	got := b.NextWithRetryAfter(12 * time.Second)
	if got != 12*time.Second {
		t.Errorf("retry-after dominant: got=%v want=%v", got, 12*time.Second)
	}
	// Second attempt: exp=2s, retryAfter=1s → expect 2s (exp dominant).
	got = b.NextWithRetryAfter(1 * time.Second)
	if got != 2*time.Second {
		t.Errorf("exp dominant: got=%v want=%v", got, 2*time.Second)
	}
	// Zero retryAfter means "no header provided" → exp wins always.
	got = b.NextWithRetryAfter(0)
	if got != 4*time.Second {
		t.Errorf("zero retry-after: got=%v want=%v", got, 4*time.Second)
	}
}

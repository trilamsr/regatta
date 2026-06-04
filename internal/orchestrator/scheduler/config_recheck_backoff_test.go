package scheduler

import "testing"

// TestSchedulerConfig_RecheckBackoffOverride asserts non-default Config knobs flow through to the helper (#794).
func TestSchedulerConfig_RecheckBackoffOverride(t *testing.T) {
	cfg := Config{
		RecheckBackoffK:             7,
		RecheckBackoffSuppressTicks: 4,
		RecheckBackoffStaleTicks:    15,
	}
	b := newRecheckBackoffWithConfig(cfg.recheckBackoffConfig())
	if b.k != 7 {
		t.Fatalf("b.k=%d; want 7 (Config override)", b.k)
	}
	if b.suppressTicks != 4 {
		t.Fatalf("b.suppressTicks=%d; want 4 (Config override)", b.suppressTicks)
	}
	if b.staleTicks != 15 {
		t.Fatalf("b.staleTicks=%d; want 15 (Config override)", b.staleTicks)
	}
}

// TestSchedulerConfig_RecheckBackoffZeroValueDefaults asserts a zero-value Config produces legacy K=3 N=10 stale=20 (#794).
func TestSchedulerConfig_RecheckBackoffZeroValueDefaults(t *testing.T) {
	cfg := Config{}
	b := newRecheckBackoffWithConfig(cfg.recheckBackoffConfig())
	if b.k != recheckBackoffDefaultK {
		t.Fatalf("b.k=%d; want %d (default)", b.k, recheckBackoffDefaultK)
	}
	if b.suppressTicks != recheckBackoffDefaultSuppressTicks {
		t.Fatalf("b.suppressTicks=%d; want %d (default)", b.suppressTicks, recheckBackoffDefaultSuppressTicks)
	}
	if b.staleTicks != recheckBackoffDefaultStaleTicks {
		t.Fatalf("b.staleTicks=%d; want %d (default)", b.staleTicks, recheckBackoffDefaultStaleTicks)
	}
}

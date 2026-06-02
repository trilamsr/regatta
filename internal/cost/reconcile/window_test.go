package reconcile

import (
	"testing"
	"time"
)

// TestReconciler_BucketWindowMatchesAnthropicSpec pins spec §3.4 line 225 — just-closed bucket window.
func TestReconciler_BucketWindowMatchesAnthropicSpec(t *testing.T) {
	cases := []struct {
		name        string
		now         time.Time
		bucketWidth time.Duration
		wantStart   time.Time
		wantEnd     time.Time
	}{
		{
			name:        "tick at 01:02 with 1h bucket fetches 00:00-01:00",
			now:         time.Date(2026, 6, 1, 1, 2, 0, 0, time.UTC),
			bucketWidth: time.Hour,
			wantStart:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:     time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC),
		},
		{
			name:        "tick exactly at top-of-hour 02:00 fetches 01:00-02:00",
			now:         time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC),
			bucketWidth: time.Hour,
			wantStart:   time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC),
			wantEnd:     time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC),
		},
		{
			name:        "tick at 14:47 with 1h bucket fetches 13:00-14:00",
			now:         time.Date(2026, 6, 1, 14, 47, 0, 0, time.UTC),
			bucketWidth: time.Hour,
			wantStart:   time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC),
			wantEnd:     time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStart, gotEnd := WindowForTick(c.now, c.bucketWidth)
			if !gotStart.Equal(c.wantStart) {
				t.Errorf("start=%v want %v", gotStart, c.wantStart)
			}
			if !gotEnd.Equal(c.wantEnd) {
				t.Errorf("end=%v want %v", gotEnd, c.wantEnd)
			}
		})
	}
}

// TestNextTickTime_AlignsToTopOfHourPlusJitter pins spec §3.4 line 225 — top-of-hour + jitter.
func TestNextTickTime_AlignsToTopOfHourPlusJitter(t *testing.T) {
	now := time.Date(2026, 6, 1, 1, 30, 0, 0, time.UTC)
	got := NextTickTime(now, time.Hour, 2*time.Minute)
	want := time.Date(2026, 6, 1, 2, 2, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextTickTime=%v want %v", got, want)
	}
}

// TestNextTickTime_AlreadyPastJitterSkipsToNextWindow pins past-jitter-mark advance behaviour.
func TestNextTickTime_AlreadyPastJitterSkipsToNextWindow(t *testing.T) {
	now := time.Date(2026, 6, 1, 2, 5, 0, 0, time.UTC) // past 02:02 already
	got := NextTickTime(now, time.Hour, 2*time.Minute)
	want := time.Date(2026, 6, 1, 3, 2, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextTickTime=%v want %v", got, want)
	}
}

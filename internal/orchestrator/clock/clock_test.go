package clock

import (
	"testing"
	"time"
)

func TestSystemClockReturnsRecentNow(t *testing.T) {
	c := System()
	before := time.Now()
	got := c.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("SystemClock.Now()=%v outside [%v, %v]", got, before, after)
	}
}

func TestFakeClockReturnsFixedTime(t *testing.T) {
	want := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	c := Fake(want)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("FakeClock.Now()=%v want %v", got, want)
	}
}

func TestFakeClockAdvance(t *testing.T) {
	start := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	c := Fake(start)
	c.Advance(5 * time.Second)
	want := start.Add(5 * time.Second)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(5s) Now()=%v want %v", got, want)
	}
}

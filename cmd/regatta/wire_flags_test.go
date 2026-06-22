package main

import (
	"strings"
	"testing"
	"time"
)

// TestValidateServeFlags_RejectsNonPositiveDurations asserts --poll / --tick / --heartbeat / --lock-ttl reject 0 or negative (R16-Bug-1; orchestrator silently substitutes defaults today, hiding operator typos).
func TestValidateServeFlags_RejectsNonPositiveDurations(t *testing.T) {
	cases := []struct {
		name  string
		f     serveFlags
		field string
	}{
		{"poll_zero", serveFlags{PollDur: 0, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: time.Minute}, "--poll"},
		{"poll_negative", serveFlags{PollDur: -1, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: time.Minute}, "--poll"},
		{"tick_zero", serveFlags{PollDur: 30 * time.Second, TickDur: 0, HeartDur: time.Minute, LockTTL: time.Minute}, "--tick"},
		{"heart_negative", serveFlags{PollDur: 30 * time.Second, TickDur: 5 * time.Second, HeartDur: -1, LockTTL: time.Minute}, "--heartbeat"},
		{"lock_ttl_zero", serveFlags{PollDur: 30 * time.Second, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: 0}, "--lock-ttl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServeFlags(tc.f)
			if err == nil {
				t.Fatalf("want err mentioning %s; got nil", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("err = %q; want mention of %s", err, tc.field)
			}
		})
	}
}

// TestValidateServeFlags_AcceptsPositives asserts positive values pass.
func TestValidateServeFlags_AcceptsPositives(t *testing.T) {
	f := serveFlags{PollDur: 30 * time.Second, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: 15 * time.Minute}
	if err := validateServeFlags(f); err != nil {
		t.Fatalf("positive flags rejected: %v", err)
	}
}

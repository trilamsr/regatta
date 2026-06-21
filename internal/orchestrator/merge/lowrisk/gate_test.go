package lowrisk

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestGate_FetchFailureHoldsClosed asserts a PRFetcher error holds the PR (false, "fetch_failed") (MAY-86).
func TestGate_FetchFailureHoldsClosed(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	g := NewGate(c, func(context.Context, int, string) (PR, error) {
		return PR{}, errors.New("gh down")
	})
	eligible, reason := g.Eligible(context.Background(), 1, "sha")
	if eligible || reason != "fetch_failed" {
		t.Fatalf("eligible=%v reason=%q; want held fetch_failed", eligible, reason)
	}
}

// TestGate_EligibleWhenClassifierApproves asserts the Gate forwards an eligible classification on fetch success (MAY-86).
func TestGate_EligibleWhenClassifierApproves(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	g := NewGate(c, func(context.Context, int, string) (PR, error) {
		return PR{ChangedPaths: []string{"docs/x.md"}, DiffLOC: 3, OpenedAt: now.Add(-time.Hour)}, nil
	})
	eligible, reason := g.Eligible(context.Background(), 1, "sha")
	if !eligible || reason != "eligible" {
		t.Fatalf("eligible=%v reason=%q; want eligible", eligible, reason)
	}
}

// TestHoldAll_HoldsEverything asserts the conservative-default gate holds every PR (false, "low_risk_disabled") (MAY-86).
func TestHoldAll_HoldsEverything(t *testing.T) {
	eligible, reason := HoldAll{}.Eligible(context.Background(), 1, "sha")
	if eligible || reason != "low_risk_disabled" {
		t.Fatalf("eligible=%v reason=%q; want held low_risk_disabled", eligible, reason)
	}
}

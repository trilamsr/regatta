package lowrisk

import (
	"context"
	"testing"
	"time"
)

// TestGate_AuditSinkFiresOnHold asserts the AuditSink receives one decision row when the gate HOLDS a PR (MAY-270).
func TestGate_AuditSinkFiresOnHold(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	loadBearing := PR{
		ChangedPaths: []string{"internal/orchestrator/foo.go"},
		DiffLOC:      1,
		OpenedAt:     now.Add(-24 * time.Hour),
	}
	var got []AuditDecision
	g := NewGate(c, func(context.Context, int, string) (PR, error) { return loadBearing, nil })
	g.SetAuditSink(func(_ context.Context, d AuditDecision) {
		got = append(got, d)
	})
	eligible, reason := g.Eligible(context.Background(), 42, "shaHold")
	if eligible || reason != ReasonLoadBearing {
		t.Fatalf("eligible=%v reason=%q; want held load_bearing_path", eligible, reason)
	}
	if len(got) != 1 {
		t.Fatalf("audit sink called %d times; want 1", len(got))
	}
	d := got[0]
	if d.PRNumber != 42 {
		t.Errorf("pr=%d; want 42", d.PRNumber)
	}
	if d.Eligible {
		t.Errorf("eligible=true; want false (held)")
	}
	if d.Reason != ReasonLoadBearing {
		t.Errorf("reason=%q; want %q", d.Reason, ReasonLoadBearing)
	}
	if d.LOCDelta != 1 {
		t.Errorf("loc_delta=%d; want 1", d.LOCDelta)
	}
	if d.SoakRemainingSec != 0 {
		t.Errorf("soak_remaining_sec=%d; want 0 (already soaked)", d.SoakRemainingSec)
	}
}

// TestGate_AuditSinkFiresOnEligible asserts the AuditSink receives one decision row when the gate APPROVES a PR (MAY-270).
func TestGate_AuditSinkFiresOnEligible(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	docOnly := PR{
		ChangedPaths: []string{"docs/readme.md"},
		DiffLOC:      3,
		OpenedAt:     now.Add(-time.Hour),
	}
	var got []AuditDecision
	g := NewGate(c, func(context.Context, int, string) (PR, error) { return docOnly, nil })
	g.SetAuditSink(func(_ context.Context, d AuditDecision) {
		got = append(got, d)
	})
	eligible, reason := g.Eligible(context.Background(), 99, "shaPass")
	if !eligible || reason != ReasonEligible {
		t.Fatalf("eligible=%v reason=%q; want eligible", eligible, reason)
	}
	if len(got) != 1 {
		t.Fatalf("audit sink called %d times; want 1", len(got))
	}
	d := got[0]
	if d.PRNumber != 99 {
		t.Errorf("pr=%d; want 99", d.PRNumber)
	}
	if !d.Eligible {
		t.Errorf("eligible=false; want true")
	}
	if d.Reason != ReasonEligible {
		t.Errorf("reason=%q; want %q", d.Reason, ReasonEligible)
	}
	if d.LOCDelta != 3 {
		t.Errorf("loc_delta=%d; want 3", d.LOCDelta)
	}
	if d.SoakRemainingSec != 0 {
		t.Errorf("soak_remaining_sec=%d; want 0 (over-soaked)", d.SoakRemainingSec)
	}
}

// TestGate_AuditSinkReportsSoakRemaining asserts the sink carries positive soak_remaining_sec for an un-soaked PR (MAY-270).
func TestGate_AuditSinkReportsSoakRemaining(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	unsoaked := PR{
		ChangedPaths: []string{"docs/x.md"},
		DiffLOC:      4,
		OpenedAt:     now.Add(-5 * time.Minute), // 10 min still needed
	}
	var got []AuditDecision
	g := NewGate(c, func(context.Context, int, string) (PR, error) { return unsoaked, nil })
	g.SetAuditSink(func(_ context.Context, d AuditDecision) {
		got = append(got, d)
	})
	eligible, reason := g.Eligible(context.Background(), 1, "sha")
	if eligible || reason != ReasonNotSoaked {
		t.Fatalf("eligible=%v reason=%q; want held soak_not_satisfied", eligible, reason)
	}
	if len(got) != 1 {
		t.Fatalf("audit sink called %d times; want 1", len(got))
	}
	if got[0].SoakRemainingSec != 600 {
		t.Errorf("soak_remaining_sec=%d; want 600 (10 min)", got[0].SoakRemainingSec)
	}
}

// TestGate_AuditSinkFiresOnFetchFail asserts the sink fires with reason=fetch_failed when the PRFetcher errors (MAY-270).
func TestGate_AuditSinkFiresOnFetchFail(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	var got []AuditDecision
	g := NewGate(c, func(context.Context, int, string) (PR, error) {
		return PR{}, errFakeFetch
	})
	g.SetAuditSink(func(_ context.Context, d AuditDecision) {
		got = append(got, d)
	})
	eligible, reason := g.Eligible(context.Background(), 7, "sha7")
	if eligible || reason != "fetch_failed" {
		t.Fatalf("eligible=%v reason=%q; want held fetch_failed", eligible, reason)
	}
	if len(got) != 1 {
		t.Fatalf("audit sink called %d times; want 1", len(got))
	}
	if got[0].Reason != "fetch_failed" || got[0].PRNumber != 7 {
		t.Errorf("got=%+v; want {PR:7, Reason:fetch_failed}", got[0])
	}
}

var errFakeFetch = fakeFetchErr("gh down")

type fakeFetchErr string

func (f fakeFetchErr) Error() string { return string(f) }

// TestGate_NoAuditSinkIsByteEquivalent asserts a nil sink keeps the pre-MAY-270 path: Eligible returns the same (verdict, reason) tuple (MAY-270).
func TestGate_NoAuditSinkIsByteEquivalent(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})
	g := NewGate(c, func(context.Context, int, string) (PR, error) {
		return PR{ChangedPaths: []string{"docs/x.md"}, DiffLOC: 3, OpenedAt: now.Add(-time.Hour)}, nil
	})
	// No SetAuditSink call. Must not panic; verdict unchanged.
	eligible, reason := g.Eligible(context.Background(), 1, "sha")
	if !eligible || reason != ReasonEligible {
		t.Fatalf("eligible=%v reason=%q; want eligible", eligible, reason)
	}
}

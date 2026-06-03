package selfimprove

import (
	"testing"
	"time"
)

// fixedNow is the deterministic clock anchor every rule test uses so
// window-cutoff arithmetic is reproducible.
var fixedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func mkEvent(id, kind string, ago time.Duration, opts ...func(*Event)) Event {
	e := Event{ID: id, Kind: kind, OccurredAt: fixedNow.Add(-ago)}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func findByRule(fs []Finding, name string) *Finding {
	for i := range fs {
		if fs[i].Rule == name {
			return &fs[i]
		}
	}
	return nil
}

// TestRule_L4RejectionStreak_DetectsThreeInWindow asserts R1 fires on three same-author+reason events.
func TestRule_L4RejectionStreak_DetectsThreeInWindow(t *testing.T) {
	rules := DefaultRules()
	events := []Event{
		mkEvent("1", "l4_reject", 1*time.Hour, func(e *Event) { e.Author = "alice"; e.Reason = "noop" }),
		mkEvent("2", "l4_reject", 2*time.Hour, func(e *Event) { e.Author = "alice"; e.Reason = "noop" }),
		mkEvent("3", "l4_reject", 3*time.Hour, func(e *Event) { e.Author = "alice"; e.Reason = "noop" }),
	}
	got := rules[0].Match(events, fixedNow)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	if got[0].Count != 3 {
		t.Fatalf("want count=3, got %d", got[0].Count)
	}
}

// TestRule_L4RejectionStreak_DifferentAuthors_NoTrigger asserts distinct authors do not fingerprint together.
func TestRule_L4RejectionStreak_DifferentAuthors_NoTrigger(t *testing.T) {
	rules := DefaultRules()
	events := []Event{
		mkEvent("1", "l4_reject", 1*time.Hour, func(e *Event) { e.Author = "alice"; e.Reason = "noop" }),
		mkEvent("2", "l4_reject", 2*time.Hour, func(e *Event) { e.Author = "bob"; e.Reason = "noop" }),
		mkEvent("3", "l4_reject", 3*time.Hour, func(e *Event) { e.Author = "carol"; e.Reason = "noop" }),
	}
	got := rules[0].Match(events, fixedNow)
	if len(got) != 0 {
		t.Fatalf("want 0 findings (different authors), got %d", len(got))
	}
}

// TestRule_CIFailStreak_5xSameCheck_Triggers asserts R2 fires at threshold=5.
func TestRule_CIFailStreak_5xSameCheck_Triggers(t *testing.T) {
	rules := DefaultRules()
	events := []Event{
		mkEvent("1", "ci_failed", 1*time.Hour, func(e *Event) { e.CheckName = "lint" }),
		mkEvent("2", "ci_failed", 2*time.Hour, func(e *Event) { e.CheckName = "lint" }),
		mkEvent("3", "ci_failed", 3*time.Hour, func(e *Event) { e.CheckName = "lint" }),
		mkEvent("4", "ci_failed", 4*time.Hour, func(e *Event) { e.CheckName = "lint" }),
		mkEvent("5", "ci_failed", 5*time.Hour, func(e *Event) { e.CheckName = "lint" }),
	}
	got := rules[1].Match(events, fixedNow)
	if f := findByRule(got, RuleCIFailStreak); f == nil || f.Count != 5 {
		t.Fatalf("want CI-fail-streak count=5, got %+v", got)
	}
}

// TestRule_ReaperKillRecurrent_3xSameAgent_Triggers asserts R3 fires at threshold=3.
func TestRule_ReaperKillRecurrent_3xSameAgent_Triggers(t *testing.T) {
	rules := DefaultRules()
	events := []Event{
		mkEvent("1", "reaper_killed", 1*time.Hour, func(e *Event) { e.AgentID = "agent-7" }),
		mkEvent("2", "reaper_killed", 2*time.Hour, func(e *Event) { e.AgentID = "agent-7" }),
		mkEvent("3", "reaper_killed", 3*time.Hour, func(e *Event) { e.AgentID = "agent-7" }),
	}
	got := rules[2].Match(events, fixedNow)
	if f := findByRule(got, RuleReaperRecurrent); f == nil || f.Count != 3 {
		t.Fatalf("want reaper-kill count=3, got %+v", got)
	}
}

// TestRule_CostCapBreach_NBreaches_Triggers asserts R4 fires at threshold=3 breaches.
func TestRule_CostCapBreach_NBreaches_Triggers(t *testing.T) {
	rules := DefaultRules()
	events := []Event{
		mkEvent("1", "cost_cap_breach", 1*time.Hour),
		mkEvent("2", "cost_cap_breach", 2*time.Hour),
		mkEvent("3", "cost_cap_breach", 3*time.Hour),
	}
	got := rules[3].Match(events, fixedNow)
	if f := findByRule(got, RuleCostCapBreach); f == nil || f.Count != 3 {
		t.Fatalf("want cost-cap-breach count=3, got %+v", got)
	}
}

// TestRule_ReviewerFindingStorm_10x_Triggers asserts R5 fires at threshold=10 same severity+scope.
func TestRule_ReviewerFindingStorm_10x_Triggers(t *testing.T) {
	rules := DefaultRules()
	var events []Event
	for i := 0; i < 10; i++ {
		e := mkEvent("e", "reviewer_finding", time.Duration(i)*time.Hour, func(e *Event) {
			e.Severity = "med"
			e.Scope = "go"
		})
		events = append(events, e)
	}
	got := rules[4].Match(events, fixedNow)
	if f := findByRule(got, RuleReviewerFindStorm); f == nil || f.Count != 10 {
		t.Fatalf("want reviewer-finding-storm count=10, got %+v", got)
	}
}

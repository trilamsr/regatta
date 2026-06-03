package selfimprove

import (
	"testing"
	"time"
)

// fixedNow anchors every rule test so window-cutoff arithmetic is deterministic.
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

// ruleByName returns the registered rule named n; testing helper.
func ruleByName(t *testing.T, n string) Rule {
	t.Helper()
	for _, r := range DefaultRules() {
		if r.Name() == n {
			return r
		}
	}
	t.Fatalf("rule %q not registered", n)
	return nil
}

// TestRule_SameGateFailRepeats_7d_3x_Triggers asserts R1 fires at 3 same-gate_kind+gate_reason events in 7d (spec §5.2).
func TestRule_SameGateFailRepeats_7d_3x_Triggers(t *testing.T) {
	r := ruleByName(t, RuleSameGateFailRepeats)
	events := []Event{
		mkEvent("1", "gate_fail", 1*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
		mkEvent("2", "gate_fail", 24*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
		mkEvent("3", "gate_fail", 48*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
	}
	got := r.Match(events, fixedNow)
	if f := findByRule(got, RuleSameGateFailRepeats); f == nil || f.Count != 3 {
		t.Fatalf("want R1 count=3, got %+v", got)
	}
}

// TestRule_SameGateFailRepeats_DifferentReasons_NoTrigger asserts R1 buckets by gate_kind+gate_reason tuple.
func TestRule_SameGateFailRepeats_DifferentReasons_NoTrigger(t *testing.T) {
	r := ruleByName(t, RuleSameGateFailRepeats)
	events := []Event{
		mkEvent("1", "gate_fail", 1*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
		mkEvent("2", "gate_fail", 24*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "stale-todo" }),
		mkEvent("3", "gate_fail", 48*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "scorecard" }),
	}
	if got := r.Match(events, fixedNow); len(got) != 0 {
		t.Fatalf("want 0 findings (distinct reasons), got %d", len(got))
	}
}

// TestRule_BannedPhraseRecurrence_7d_2x_Triggers asserts R2 fires at 2 same-banned_token doc_check_failed in 7d (spec §5.2).
func TestRule_BannedPhraseRecurrence_7d_2x_Triggers(t *testing.T) {
	r := ruleByName(t, RuleBannedPhraseRecurrence)
	events := []Event{
		mkEvent("1", "doc_check_failed", 1*time.Hour, func(e *Event) { e.BannedToken = "blazing-fast" }),
		mkEvent("2", "doc_check_failed", 72*time.Hour, func(e *Event) { e.BannedToken = "blazing-fast" }),
	}
	got := r.Match(events, fixedNow)
	if f := findByRule(got, RuleBannedPhraseRecurrence); f == nil || f.Count != 2 {
		t.Fatalf("want R2 count=2, got %+v", got)
	}
}

// TestRule_SubagentClaimedCleanButCIFailed_7d_2x_Triggers asserts R3 fires at threshold within 7d (spec §5.2 threshold=3).
func TestRule_SubagentClaimedCleanButCIFailed_7d_2x_Triggers(t *testing.T) {
	r := ruleByName(t, RuleSubagentClaimedCleanButCIFailed)
	events := []Event{
		mkEvent("1", "subagent_claim", 1*time.Hour, func(e *Event) { e.ClaimText = "make check clean"; e.FailureKind = "lint" }),
		mkEvent("2", "ci_failed", 2*time.Hour, func(e *Event) { e.ClaimText = "make check clean"; e.FailureKind = "lint" }),
		mkEvent("3", "ci_failed", 50*time.Hour, func(e *Event) { e.ClaimText = "make check clean"; e.FailureKind = "lint" }),
	}
	got := r.Match(events, fixedNow)
	if f := findByRule(got, RuleSubagentClaimedCleanButCIFailed); f == nil || f.Count != 3 {
		t.Fatalf("want R3 count=3, got %+v", got)
	}
}

// TestRule_LoadBearingLeftoverPattern_14d_2x_Triggers asserts R4 fires at 2 same leftover_pattern in 14d (spec §5.2).
func TestRule_LoadBearingLeftoverPattern_14d_2x_Triggers(t *testing.T) {
	r := ruleByName(t, RuleLoadBearingLeftoverPattern)
	events := []Event{
		mkEvent("1", "pr_body_scan", 1*24*time.Hour, func(e *Event) { e.LeftoverPattern = "TODO: file followup" }),
		mkEvent("2", "pr_body_scan", 10*24*time.Hour, func(e *Event) { e.LeftoverPattern = "TODO: file followup" }),
	}
	got := r.Match(events, fixedNow)
	if f := findByRule(got, RuleLoadBearingLeftoverPattern); f == nil || f.Count != 2 {
		t.Fatalf("want R4 count=2, got %+v", got)
	}
}

// TestRule_LoadBearingLeftoverPattern_OutsideWindow_NoTrigger asserts events older than 14d are dropped.
func TestRule_LoadBearingLeftoverPattern_OutsideWindow_NoTrigger(t *testing.T) {
	r := ruleByName(t, RuleLoadBearingLeftoverPattern)
	events := []Event{
		mkEvent("1", "pr_body_scan", 1*24*time.Hour, func(e *Event) { e.LeftoverPattern = "TODO: file followup" }),
		mkEvent("2", "pr_body_scan", 20*24*time.Hour, func(e *Event) { e.LeftoverPattern = "TODO: file followup" }),
	}
	if got := r.Match(events, fixedNow); len(got) != 0 {
		t.Fatalf("want 0 findings (outside 14d window), got %d", len(got))
	}
}

// TestRule_ReaperKillsSameAgent_7d_2x_Triggers asserts R5 fires at spec §5.2 threshold=5 same agent_id in 7d.
func TestRule_ReaperKillsSameAgent_7d_2x_Triggers(t *testing.T) {
	r := ruleByName(t, RuleReaperKillsSameAgent)
	var events []Event
	for i := 0; i < 5; i++ {
		events = append(events, mkEvent(
			"e", "reaper_killed", time.Duration(i)*time.Hour,
			func(e *Event) { e.AgentID = "agent-7" },
		))
	}
	got := r.Match(events, fixedNow)
	if f := findByRule(got, RuleReaperKillsSameAgent); f == nil || f.Count != 5 {
		t.Fatalf("want R5 count=5, got %+v", got)
	}
}

// TestRule_PauseAllTag_FiltersOutEvents asserts spec §11 risk #4 — pause-tagged events never count toward fires.
func TestRule_PauseAllTag_FiltersOutEvents(t *testing.T) {
	r := ruleByName(t, RuleSameGateFailRepeats)
	events := []Event{
		mkEvent("1", "gate_fail", 1*time.Hour, func(e *Event) {
			e.GateKind = "pr-lint"
			e.GateReason = "banned-phrase"
			e.Tags = []string{PauseAllTag}
		}),
		mkEvent("2", "gate_fail", 2*time.Hour, func(e *Event) {
			e.GateKind = "pr-lint"
			e.GateReason = "banned-phrase"
			e.Tags = []string{PauseAllTag}
		}),
		mkEvent("3", "gate_fail", 3*time.Hour, func(e *Event) {
			e.GateKind = "pr-lint"
			e.GateReason = "banned-phrase"
			e.Tags = []string{PauseAllTag}
		}),
	}
	if got := r.Match(events, fixedNow); len(got) != 0 {
		t.Fatalf("want 0 findings (all pause-tagged), got %d", len(got))
	}
}

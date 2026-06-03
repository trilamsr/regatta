package selfimprove

import (
	"fmt"
	"time"
)

// Rule kinds align verbatim with spec §5.2's MVP table — names + windows
// + thresholds + group_by + event kinds are the contract the detector
// hashes into dedup keys; drift here drift's all open dedup-key issues
// off their fingerprint. #644 closed the prior name/threshold deviation.
const (
	RuleSameGateFailRepeats             = "same-gate-fail-repeats"
	RuleBannedPhraseRecurrence          = "banned-phrase-recurrence"
	RuleSubagentClaimedCleanButCIFailed = "subagent-claimed-clean-but-ci-failed"
	RuleLoadBearingLeftoverPattern      = "load-bearing-leftover-pattern"
	RuleReaperKillsSameAgent            = "reaper-kills-same-agent"
)

// PauseAllTag is the W5 cost-pause suppressor; every rule that could
// fire during a forced halt declares it in FilterOut so pause-induced
// halts never blame agents (spec §11 risk #4).
const PauseAllTag = "regatta_pause_all"

// Substrate event-kind names rules consume (spec §5.2). Shared
// constants keep tests + rule registry + reader on one spelling.
const (
	EventKindGateFail        = "gate_fail"
	EventKindDocCheckFailed  = "doc_check_failed"
	EventKindSubagentClaim   = "subagent_claim"
	EventKindCIFailed        = "ci_failed"
	EventKindPRBodyScan      = "pr_body_scan"
	EventKindReaperKilled    = "reaper_killed"
)

// streakRule is the shared bucketing primitive — all five spec §5.2
// rules are streak-by-fingerprint variants of one Match() loop.
type streakRule struct {
	name       string
	window     time.Duration
	threshold  int
	eventKinds []string
	filterOut  []string
	severity   string
	groupBy    func(Event) (map[string]string, string)
}

func (r *streakRule) Name() string          { return r.name }
func (r *streakRule) Window() time.Duration { return r.window }
func (r *streakRule) EventKinds() []string  { return r.eventKinds }
func (r *streakRule) FilterOut() []string   { return r.filterOut }

// Match buckets events on the rule fingerprint and emits one Finding
// per bucket crossing threshold; pause-tagged events are filtered first
// so a cost-pause storm never blames agents (spec §11 risk #4).
func (r *streakRule) Match(events []Event, now time.Time) []Finding {
	cutoff := now.Add(-r.window)
	filtered := filterOut(events, r.filterOut)

	type bucket struct {
		group    map[string]string
		subject  string
		eventIDs []string
		earliest time.Time
		latest   time.Time
	}
	buckets := make(map[string]*bucket)

	for _, e := range filtered {
		if e.OccurredAt.Before(cutoff) {
			continue
		}
		if !kindMatches(e.Kind, r.eventKinds) {
			continue
		}
		group, subject := r.groupBy(e)
		if group == nil {
			continue
		}
		key := ComputeDedupKey(r.name, group)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{group: group, subject: subject, earliest: e.OccurredAt, latest: e.OccurredAt}
			buckets[key] = b
		}
		b.eventIDs = append(b.eventIDs, e.ID)
		if e.OccurredAt.Before(b.earliest) {
			b.earliest = e.OccurredAt
		}
		if e.OccurredAt.After(b.latest) {
			b.latest = e.OccurredAt
		}
	}

	var out []Finding
	for _, b := range buckets {
		if len(b.eventIDs) < r.threshold {
			continue
		}
		out = append(out, Finding{
			Rule:        r.name,
			GroupByVals: b.group,
			Severity:    r.severity,
			Count:       len(b.eventIDs),
			DedupKey:    ComputeDedupKey(r.name, b.group),
			Subject:     b.subject,
			EventIDs:    b.eventIDs,
			WindowStart: b.earliest,
			WindowEnd:   b.latest,
		})
	}
	return out
}

func kindMatches(kind string, allowed []string) bool {
	for _, k := range allowed {
		if k == kind {
			return true
		}
	}
	return false
}

// DefaultRules returns the five MVP rules verbatim from spec §5.2's
// window/threshold table. Any drift here must amend the spec FIRST per
// `feedback_spec_pattern_authority` — implementer-side deviation is what
// #644 fixed.
func DefaultRules() []Rule {
	return []Rule{
		// R1: same-gate-fail-repeats (7d / 3) — gate_fail grouped by
		// gate_kind+gate_reason. Spec §5.2.
		&streakRule{
			name:       RuleSameGateFailRepeats,
			window:     7 * 24 * time.Hour,
			threshold:  3,
			eventKinds: []string{EventKindGateFail},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityMedium,
			groupBy: func(e Event) (map[string]string, string) {
				if e.GateKind == "" || e.GateReason == "" {
					return nil, ""
				}
				return map[string]string{"gate_kind": e.GateKind, "gate_reason": e.GateReason},
					fmt.Sprintf("%s/%s", e.GateKind, e.GateReason)
			},
		},
		// R2: banned-phrase-recurrence (7d / 2) — doc_check_failed
		// grouped by banned_token across distinct PRs. Spec §5.2.
		&streakRule{
			name:       RuleBannedPhraseRecurrence,
			window:     7 * 24 * time.Hour,
			threshold:  2,
			eventKinds: []string{EventKindDocCheckFailed},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityMedium,
			groupBy: func(e Event) (map[string]string, string) {
				if e.BannedToken == "" {
					return nil, ""
				}
				return map[string]string{"banned_token": e.BannedToken}, e.BannedToken
			},
		},
		// R3: subagent-claimed-clean-but-ci-failed (7d / 3) — pairs
		// subagent_claim with ci_failed grouped by claim_text+failure_kind.
		// Spec §5.2.
		&streakRule{
			name:       RuleSubagentClaimedCleanButCIFailed,
			window:     7 * 24 * time.Hour,
			threshold:  3,
			eventKinds: []string{EventKindSubagentClaim, EventKindCIFailed},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityHigh,
			groupBy: func(e Event) (map[string]string, string) {
				if e.ClaimText == "" || e.FailureKind == "" {
					return nil, ""
				}
				return map[string]string{"claim_text": e.ClaimText, "failure_kind": e.FailureKind},
					fmt.Sprintf("%s/%s", e.ClaimText, e.FailureKind)
			},
		},
		// R4: load-bearing-leftover-pattern (14d / 2) — pr_body_scan
		// grouped by leftover_pattern across PRs. Spec §5.2.
		&streakRule{
			name:       RuleLoadBearingLeftoverPattern,
			window:     14 * 24 * time.Hour,
			threshold:  2,
			eventKinds: []string{EventKindPRBodyScan},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityMedium,
			groupBy: func(e Event) (map[string]string, string) {
				if e.LeftoverPattern == "" {
					return nil, ""
				}
				return map[string]string{"leftover_pattern": e.LeftoverPattern}, e.LeftoverPattern
			},
		},
		// R5: reaper-kills-same-agent (7d / 5) — reaper_killed grouped
		// by agent_id. Spec §5.2 (extra-beyond-brief; observed in waves).
		&streakRule{
			name:       RuleReaperKillsSameAgent,
			window:     7 * 24 * time.Hour,
			threshold:  5,
			eventKinds: []string{EventKindReaperKilled},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityHigh,
			groupBy: func(e Event) (map[string]string, string) {
				if e.AgentID == "" {
					return nil, ""
				}
				return map[string]string{"agent_id": e.AgentID}, e.AgentID
			},
		},
	}
}

package selfimprove

import (
	"fmt"
	"time"
)

// Rule kinds (spec §5.2). Each rule = ~25 LoC; the registry returns
// the five MVP rules pre-configured with the spec's window+threshold
// table. Adding R6 is a one-file diff per acceptance criterion c3.
const (
	RuleL4Rejection       = "l4-rejection-streak"
	RuleCIFailStreak      = "ci-check-fail-streak"
	RuleReaperRecurrent   = "reaper-kill-recurrent"
	RuleCostCapBreach     = "cost-cap-breach"
	RuleReviewerFindStorm = "reviewer-finding-storm"
)

// PauseAllTag names the W5 substrate-event tag that suppresses rule
// fires during a cost-pause window (spec §11 risk #4). Every MVP rule
// declares it in FilterOut so pause-induced halts never get blamed on
// agents.
const PauseAllTag = "regatta_pause_all"

// streakRule is the shared bucketing primitive: count events grouped
// by a tuple of payload fields, fire one Finding per bucket that
// crosses threshold inside window. All five MVP rules are
// streak-by-fingerprint variants of the same shape (spec §5.1), so the
// implementation lives once.
type streakRule struct {
	name       string
	window     time.Duration
	threshold  int
	eventKinds []string
	filterOut  []string
	severity   string
	groupBy    func(Event) (map[string]string, string)
}

func (r *streakRule) Name() string         { return r.name }
func (r *streakRule) Window() time.Duration { return r.window }
func (r *streakRule) EventKinds() []string  { return r.eventKinds }
func (r *streakRule) FilterOut() []string   { return r.filterOut }

// Match groups events by the rule's fingerprint and emits one Finding
// per bucket whose count >= threshold. Pause-tagged events are
// filtered up-front so a cost-pause storm never triggers an agent-
// blaming false positive (spec §11 risk #4).
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

// DefaultRules returns the five MVP rules configured per spec §5.2's
// window/threshold table. Operator overrides land via a YAML loader
// (deferred — spec §15 followup, not blocking W4 issue-filing path).
func DefaultRules() []Rule {
	return []Rule{
		&streakRule{
			name:       RuleL4Rejection,
			window:     24 * time.Hour,
			threshold:  3,
			eventKinds: []string{"l4_reject", "gate_fail"},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityMedium,
			groupBy: func(e Event) (map[string]string, string) {
				if e.Author == "" || e.Reason == "" {
					return nil, ""
				}
				return map[string]string{"author": e.Author, "reason": e.Reason},
					fmt.Sprintf("%s/%s", e.Author, e.Reason)
			},
		},
		&streakRule{
			name:       RuleCIFailStreak,
			window:     7 * 24 * time.Hour,
			threshold:  5,
			eventKinds: []string{"ci_failed"},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityMedium,
			groupBy: func(e Event) (map[string]string, string) {
				if e.CheckName == "" {
					return nil, ""
				}
				return map[string]string{"check_name": e.CheckName}, e.CheckName
			},
		},
		&streakRule{
			name:       RuleReaperRecurrent,
			window:     7 * 24 * time.Hour,
			threshold:  3,
			eventKinds: []string{"reaper_killed"},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityHigh,
			groupBy: func(e Event) (map[string]string, string) {
				if e.AgentID == "" {
					return nil, ""
				}
				return map[string]string{"agent_id": e.AgentID}, e.AgentID
			},
		},
		&streakRule{
			name:       RuleCostCapBreach,
			window:     7 * 24 * time.Hour,
			threshold:  3,
			eventKinds: []string{"cost_cap_breach"},
			filterOut:  nil,
			severity:   SeverityHigh,
			groupBy: func(e Event) (map[string]string, string) {
				return map[string]string{"kind": "cost_cap_breach"}, "cost_cap"
			},
		},
		&streakRule{
			name:       RuleReviewerFindStorm,
			window:     14 * 24 * time.Hour,
			threshold:  10,
			eventKinds: []string{"reviewer_finding"},
			filterOut:  []string{PauseAllTag},
			severity:   SeverityMedium,
			groupBy: func(e Event) (map[string]string, string) {
				if e.Severity == "" || e.Scope == "" {
					return nil, ""
				}
				return map[string]string{"severity": e.Severity, "scope": e.Scope},
					fmt.Sprintf("%s/%s", e.Severity, e.Scope)
			},
		},
	}
}

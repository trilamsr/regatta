// Package selfimprove implements the PHASE-AUTONOMY W4 self-improvement
// detector — a rule-driven + nightly-LLM scanner that reads substrate
// events, identifies recurring failure patterns, and files
// `self-improvement` GH issues with sha256 dedup. Spec:
// docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md.
//
// Hybrid choice (spec §3.1): rules cover the four named brief triggers
// deterministically; the LLM-nightly path proposes *candidate* rules
// only (writes YAML proposals — never auto-files issues), so a model
// hallucination cannot pollute the issue tracker.
package selfimprove

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is folded into every dedup key per spec §11 risk #5; a
// rule-definition or payload-shape change forces re-fire on a new key
// instead of silently colliding with an issue filed under the old shape.
const SchemaVersion = "v1"

// LabelSelfImprovement is the GH label every detector-filed issue
// carries — the bulk dedup fetch (spec §6.1 step 4) is scoped by this
// label so the detector never has to scan the full open-issue corpus.
const LabelSelfImprovement = "self-improvement"

// Severity tiers per spec §5.4 — exposed as constants so the rules
// table + tests share one spelling and goconst stays clean.
const (
	SeverityLow    = "low"
	SeverityMedium = "medium"
	SeverityHigh   = "high"
)

// Event is the read-only view the detector takes over a substrate row.
// Fields mirror `substrate_events` columns the spec §4.1 query
// projects. Tags carry the per-rule `filter_out` axis (spec §11
// risk #4) — `regatta_pause_all` lets W5 cost-pause windows suppress
// rule fires that would otherwise blame agents for pause-induced halts.
type Event struct {
	ID         string
	OccurredAt time.Time
	Kind       string
	Author     string
	Reason     string
	CheckName  string
	AgentID    string
	Severity   string
	Scope      string
	Tags       []string
}

// HasTag reports whether the event carries the named tag — used by
// rule filter_out (spec §5.1) to skip events emitted during a
// `regatta_pause_all` window so W5 cost-pause halts do not trip R3/R5.
func (e Event) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Finding is one bucket whose count crossed a rule threshold inside
// the rule's window — the unit that becomes one GH issue (or one
// comment on the existing dedup-matched issue).
type Finding struct {
	Rule         string
	GroupByVals  map[string]string
	Severity     string
	Count        int
	DedupKey     string
	Subject      string
	EventIDs     []string
	WindowStart  time.Time
	WindowEnd    time.Time
}

// Rule is the seam every detector heuristic implements. Match runs
// over the *already-filtered* event slice (filter_out tags stripped
// upstream) and returns one Finding per bucket that crosses threshold.
type Rule interface {
	Name() string
	Window() time.Duration
	EventKinds() []string
	FilterOut() []string
	Match(events []Event, now time.Time) []Finding
}

// ComputeDedupKey is the public hashing primitive used by both Rule
// implementations and any external caller that wants to reconstruct the
// key for an existing finding — sha256(rule + sorted group_by + schema)
// matches spec §6.1 step 3.
func ComputeDedupKey(rule string, groupBy map[string]string) string {
	keys := make([]string, 0, len(groupBy))
	for k := range groupBy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(rule)
	b.WriteByte('|')
	b.WriteString(SchemaVersion)
	for _, k := range keys {
		b.WriteByte('|')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(groupBy[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// filterOut drops events whose Tags intersect any of the rule's
// filter_out tags. Centralised so every rule inherits the W5
// cost-pause coordination (spec §11 risk #4) without each Match()
// re-implementing the check.
func filterOut(events []Event, tags []string) []Event {
	if len(tags) == 0 {
		return events
	}
	out := make([]Event, 0, len(events))
	for _, e := range events {
		skip := false
		for _, t := range tags {
			if e.HasTag(t) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, e)
		}
	}
	return out
}

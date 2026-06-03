// Package triggers computes Phase GREEN-CLOCK progress — the rolling
// 30-day counter that gates regatta's Phase-S (operator-supervised) to
// Phase-G (unattended) transition. The counter increments by one on
// each "green day" (≥ N PRs merged AND zero manual_merge or
// operator_intervention events in window) and resets to zero on a
// non-green day. Reset is intentional and explicit so CI flake cannot
// silently roll back 25+ days of progress (spec §5 + R3).
package triggers

import (
	"fmt"
	"time"
)

// Event names emitted on the substrate that GreenClock reads. They are
// surfaced as constants so the substrate reducer and the clock share
// one source of truth.
const (
	EventPRMerged             = "agent_pr_merged"
	EventManualMerge          = "manual_merge"
	EventOperatorIntervention = "operator_intervention"
)

// SubstrateEvent is the minimal projection GreenClock needs from a
// substrate read. Callers adapt their own event row type to this
// struct; the package stays decoupled from the storage layer.
type SubstrateEvent struct {
	Kind string
	At   time.Time
}

// Config tunes the GreenClock invariants. Zero-value defaults match
// the spec defaults (10 PRs/day, 30-day window, UTC). Callers
// override via slo/triggers.yaml — see LoadConfig.
type Config struct {
	// ThresholdPRsPerDay is the minimum merged-PR count that
	// qualifies a day as green. Defaults to 10 (spec §5.2).
	ThresholdPRsPerDay int
	// WindowDays is the consecutive-green-day target. Defaults to
	// 30 (spec §5.2).
	WindowDays int
	// TZ is the operator's wall-clock for day rollover. Defaults to
	// UTC. PT/JST/etc. are valid IANA names per slo/triggers.yaml
	// (spec §5.2 + R6).
	TZ *time.Location
}

// withDefaults returns c with zero fields filled from the spec
// defaults. Internal — Config callers expect Compute to apply this.
func (c Config) withDefaults() Config {
	if c.ThresholdPRsPerDay == 0 {
		c.ThresholdPRsPerDay = 10
	}
	if c.WindowDays == 0 {
		c.WindowDays = 30
	}
	if c.TZ == nil {
		c.TZ = time.UTC
	}
	return c
}

// State is the GreenClock snapshot at a given wall-clock instant.
// DayCount is the current consecutive-green-day count; DaysRemaining
// is WindowDays - DayCount clamped at zero. LastReset is the start
// of the most recent non-green day (or the zero value if the counter
// has never reset since collection start).
type State struct {
	DayCount       int
	DaysRemaining  int
	LastReset      time.Time
	WindowComplete bool
}

// Compute walks events newest-to-oldest, partitions by local day,
// classifies each day as green or non-green, and counts the
// consecutive trailing green streak. "now" sets the right edge of
// the window so tests stay deterministic.
func Compute(cfg Config, events []SubstrateEvent, now time.Time) State {
	cfg = cfg.withDefaults()
	now = now.In(cfg.TZ)

	// Bucket events by local day; map key is the YYYY-MM-DD string in
	// cfg.TZ so the day partition matches the operator's wall clock,
	// not the server's.
	type dayCounts struct {
		merged           int
		manualInterv     int
	}
	buckets := map[string]*dayCounts{}
	dayKey := func(t time.Time) string {
		return t.In(cfg.TZ).Format("2006-01-02")
	}
	for _, e := range events {
		key := dayKey(e.At)
		b, ok := buckets[key]
		if !ok {
			b = &dayCounts{}
			buckets[key] = b
		}
		switch e.Kind {
		case EventPRMerged:
			b.merged++
		case EventManualMerge, EventOperatorIntervention:
			b.manualInterv++
		}
	}

	// Walk backward from the day BEFORE today (today is in-flight and
	// stays "pending" — the operator should not see day-N flip until
	// the day rolls over). The trailing streak of green days is the
	// counter.
	yesterday := now.AddDate(0, 0, -1)
	streak := 0
	var lastReset time.Time
	for i := 0; i < cfg.WindowDays; i++ {
		day := yesterday.AddDate(0, 0, -i)
		key := dayKey(day)
		b, ok := buckets[key]
		if !ok {
			b = &dayCounts{}
		}
		if b.merged >= cfg.ThresholdPRsPerDay && b.manualInterv == 0 {
			streak++
			continue
		}
		// First non-green day breaks the streak; record its start as
		// the last-reset boundary for the dashboard tooltip.
		lastReset = day
		break
	}

	s := State{
		DayCount:       streak,
		DaysRemaining:  cfg.WindowDays - streak,
		LastReset:      lastReset,
		WindowComplete: streak >= cfg.WindowDays,
	}
	if s.DaysRemaining < 0 {
		s.DaysRemaining = 0
	}
	return s
}

// LoadTZ resolves an IANA timezone name to a *time.Location. Empty
// name returns UTC. Used by config loaders so a misconfigured TZ
// surfaces with a clear error (spec R6).
func LoadTZ(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("triggers: bad timezone %q: %w", name, err)
	}
	return loc, nil
}

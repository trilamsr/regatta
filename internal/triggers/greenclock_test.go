package triggers

import (
	"path/filepath"
	"testing"
	"time"
)

// makeGreenDayEvents builds 10 PR-merged events spread through a single calendar day in tz.
func makeGreenDayEvents(day time.Time, tz *time.Location, count int) []SubstrateEvent {
	events := make([]SubstrateEvent, count)
	for i := 0; i < count; i++ {
		events[i] = SubstrateEvent{
			Kind: EventPRMerged,
			At:   time.Date(day.Year(), day.Month(), day.Day(), 6+i%12, 0, 0, 0, tz),
		}
	}
	return events
}

// TestGreenClock_30DayWindow_Increments confirms 30 consecutive green days yield DayCount=30 and WindowComplete=true.
func TestGreenClock_30DayWindow_Increments(t *testing.T) {
	tz := time.UTC
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, tz)
	var events []SubstrateEvent
	for i := 1; i <= 30; i++ {
		day := now.AddDate(0, 0, -i)
		events = append(events, makeGreenDayEvents(day, tz, 10)...)
	}
	s := Compute(Config{TZ: tz}, events, now)
	if s.DayCount != 30 {
		t.Errorf("DayCount = %d; want 30", s.DayCount)
	}
	if !s.WindowComplete {
		t.Error("WindowComplete = false; want true")
	}
	if s.DaysRemaining != 0 {
		t.Errorf("DaysRemaining = %d; want 0", s.DaysRemaining)
	}
}

// TestGreenClock_NoResetOnFlake confirms auto-retry merges keep the streak intact (no manual_merge => no reset).
func TestGreenClock_NoResetOnFlake(t *testing.T) {
	tz := time.UTC
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, tz)
	var events []SubstrateEvent
	// 25 green days; the 13th day had a CI flake retried-then-passed
	// — still 10 auto merges, no manual_merge event.
	for i := 1; i <= 25; i++ {
		day := now.AddDate(0, 0, -i)
		events = append(events, makeGreenDayEvents(day, tz, 10)...)
	}
	s := Compute(Config{TZ: tz}, events, now)
	if s.DayCount != 25 {
		t.Errorf("DayCount = %d; want 25 (flake should not reset)", s.DayCount)
	}
}

// TestGreenClock_ExplicitOperatorIntervention_Resets confirms a single manual_merge event breaks the streak.
func TestGreenClock_ExplicitOperatorIntervention_Resets(t *testing.T) {
	tz := time.UTC
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, tz)
	var events []SubstrateEvent
	// 5 green days, then yesterday had a manual_merge override.
	for i := 1; i <= 5; i++ {
		day := now.AddDate(0, 0, -i)
		events = append(events, makeGreenDayEvents(day, tz, 10)...)
	}
	yesterday := now.AddDate(0, 0, -1)
	events = append(events, SubstrateEvent{
		Kind: EventManualMerge,
		At:   time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 10, 0, 0, 0, tz),
	})
	s := Compute(Config{TZ: tz}, events, now)
	if s.DayCount != 0 {
		t.Errorf("DayCount = %d; want 0 after manual_merge", s.DayCount)
	}
}

// TestGreenClock_BelowThreshold_Resets confirms a day with < threshold merges breaks the streak.
func TestGreenClock_BelowThreshold_Resets(t *testing.T) {
	tz := time.UTC
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, tz)
	var events []SubstrateEvent
	for i := 1; i <= 5; i++ {
		day := now.AddDate(0, 0, -i)
		count := 10
		if i == 3 {
			count = 5 // below threshold
		}
		events = append(events, makeGreenDayEvents(day, tz, count)...)
	}
	s := Compute(Config{TZ: tz}, events, now)
	if s.DayCount != 2 {
		t.Errorf("DayCount = %d; want 2 (days 1+2 green, day 3 below threshold)", s.DayCount)
	}
}

// TestGreenClock_TimezoneRollover proves UTC + PT + JST partition events on different day boundaries.
func TestGreenClock_TimezoneRollover(t *testing.T) {
	pt, _ := time.LoadLocation("America/Los_Angeles")
	jst, _ := time.LoadLocation("Asia/Tokyo")
	zones := []*time.Location{time.UTC, pt, jst}
	for _, tz := range zones {
		now := time.Date(2026, 6, 2, 12, 0, 0, 0, tz)
		var events []SubstrateEvent
		for i := 1; i <= 3; i++ {
			day := now.AddDate(0, 0, -i)
			events = append(events, makeGreenDayEvents(day, tz, 10)...)
		}
		s := Compute(Config{TZ: tz}, events, now)
		if s.DayCount != 3 {
			t.Errorf("tz=%s DayCount = %d; want 3", tz, s.DayCount)
		}
	}
}

// TestGreenClock_DSTSpringForward_HandlesGracefully covers the 2026-03-08 PT spring-forward boundary without losing a day.
func TestGreenClock_DSTSpringForward_HandlesGracefully(t *testing.T) {
	pt, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("PT zone unavailable: %v", err)
	}
	// "now" placed just after DST spring-forward; window covers both
	// sides of the transition.
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, pt)
	var events []SubstrateEvent
	for i := 1; i <= 10; i++ {
		day := now.AddDate(0, 0, -i)
		events = append(events, makeGreenDayEvents(day, pt, 10)...)
	}
	s := Compute(Config{TZ: pt}, events, now)
	if s.DayCount != 10 {
		t.Errorf("DayCount = %d across DST spring-forward; want 10", s.DayCount)
	}
}

// TestLoadFile_ParsesTriggersYAML confirms the YAML loader decodes the spec-shaped config.
func TestLoadFile_ParsesTriggersYAML(t *testing.T) {
	// Resolve repo root relative to this test file so the assertion
	// runs regardless of go test cwd quirks.
	path := filepath.Join("..", "..", "slo", "triggers.yaml")
	f, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := f.Triggers["30_day_green"]; !ok {
		t.Error("missing 30_day_green trigger")
	}
	if f.Timezone == "" {
		t.Error("missing timezone field")
	}
	cfg, err := f.ConfigFor("30_day_green")
	if err != nil {
		t.Fatalf("ConfigFor: %v", err)
	}
	if cfg.ThresholdPRsPerDay != 10 || cfg.WindowDays != 30 {
		t.Errorf("ConfigFor = %+v; want threshold=10 window=30", cfg)
	}
}

// TestLoadTZ_BadName surfaces a clear error on a misconfigured timezone.
func TestLoadTZ_BadName(t *testing.T) {
	if _, err := LoadTZ("Not/A/Real/Zone"); err == nil {
		t.Error("LoadTZ accepted a bogus zone name")
	}
}

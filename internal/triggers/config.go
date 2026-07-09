package triggers

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// TriggerSpec is one entry in slo/triggers.yaml. Operator-editable;
// CUE schema at slo/triggers.cue validates structural drift on
// make slo-compile.
type TriggerSpec struct {
	StartDate          string `yaml:"start_date"`
	WindowDays         int    `yaml:"window_days"`
	ThresholdPRsPerDay int    `yaml:"threshold_prs_per_day"`
	Activated          *bool  `yaml:"activated,omitempty"`
}

// File mirrors the top-level shape of slo/triggers.yaml. Unknown keys
// reject on decode so a typo in the config does not silently disable
// a trigger.
type File struct {
	Triggers map[string]TriggerSpec `yaml:"triggers"`
	Timezone string                 `yaml:"timezone"`
}

// LoadFile reads + decodes a triggers.yaml from disk. Returns an
// error on missing file, invalid YAML, or unknown TZ so the operator
// hears about misconfig at boot instead of via a stuck gauge.
func LoadFile(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path) //nolint:gosec // path is an operator-supplied config file, intentional
	if err != nil {
		return f, fmt.Errorf("triggers: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("triggers: parse %s: %w", path, err)
	}
	if _, err := LoadTZ(f.Timezone); err != nil {
		return f, err
	}
	return f, nil
}

// ConfigFor projects one TriggerSpec into the Compute Config shape.
// The TZ comes from the file-level timezone field so all triggers in
// the same file roll over on one wall clock.
func (f File) ConfigFor(name string) (Config, error) {
	spec, ok := f.Triggers[name]
	if !ok {
		return Config{}, fmt.Errorf("triggers: unknown trigger %q", name)
	}
	tz, err := LoadTZ(f.Timezone)
	if err != nil {
		return Config{}, err
	}
	return Config{
		ThresholdPRsPerDay: spec.ThresholdPRsPerDay,
		WindowDays:         spec.WindowDays,
		TZ:                 tz,
	}, nil
}

// StartTime parses the spec's start_date in the file's timezone.
// Empty string returns the zero time so callers can branch on
// !t.IsZero() before treating it as a window boundary.
func (s TriggerSpec) StartTime(tz *time.Location) (time.Time, error) {
	if s.StartDate == "" {
		return time.Time{}, nil
	}
	if tz == nil {
		tz = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02", s.StartDate, tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("triggers: bad start_date %q: %w", s.StartDate, err)
	}
	return t, nil
}

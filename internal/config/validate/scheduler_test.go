package validate

import (
	"strings"
	"testing"
)

// TestLoadConfig_SchedulerParallelCap_RoundTrips asserts spec
// 2026-06-09-scheduler-parallel-cap-enforcement §3.1: a yaml block
// `scheduler: { parallel_cap: N }` decodes into Config.Scheduler.ParallelCap.
func TestLoadConfig_SchedulerParallelCap_RoundTrips(t *testing.T) {
	yaml := strings.Replace(minimalValid, "safety:", `scheduler:
  parallel_cap: 6
safety:`, 1)
	cfg, err := LoadConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got, want := cfg.SchedulerParallelCap(), 6; got != want {
		t.Fatalf("SchedulerParallelCap=%d, want %d", got, want)
	}
}

// TestLoadConfig_SchedulerAbsent_DefaultsZero asserts the absent-block
// path: pre-#1169 yaml (no scheduler key) keeps ParallelCap=0 (disabled,
// lane-cap-only). Backward-compat for shipped configs.
func TestLoadConfig_SchedulerAbsent_DefaultsZero(t *testing.T) {
	cfg, err := LoadConfig([]byte(minimalValid))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.SchedulerParallelCap(); got != 0 {
		t.Fatalf("SchedulerParallelCap=%d, want 0 (absent scheduler block)", got)
	}
}

// TestLoadConfig_SchedulerOverCap_Rejected asserts the CUE upper bound
// (16) fires on operator typo.
func TestLoadConfig_SchedulerOverCap_Rejected(t *testing.T) {
	yaml := strings.Replace(minimalValid, "safety:", `scheduler:
  parallel_cap: 999
safety:`, 1)
	if _, err := LoadConfig([]byte(yaml)); err == nil {
		t.Fatal("expected error for parallel_cap=999 (>16); got nil")
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/triggers"
)

// TestTriggers_DefaultConfigPathResolvesAgainstRepoRoot asserts the default --config path resolves against the repo root (not orphan config/triggers/).
func TestTriggers_DefaultConfigPathResolvesAgainstRepoRoot(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			t.Fatalf("repo root not found from %q (no go.mod ancestor)", cwd)
		}
		repoRoot = parent
	}
	const defaultConfig = "slo/triggers.yaml"
	path := filepath.Join(repoRoot, defaultConfig)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("triggers default config path %q does not resolve from repo root %q: %v", defaultConfig, repoRoot, err)
	}
}

// TestTriggers_BuildRowsSortsByName pins deterministic row ordering on map iteration (#636).
func TestTriggers_BuildRowsSortsByName(t *testing.T) {
	f := triggers.File{
		Triggers: map[string]triggers.TriggerSpec{
			"zeta":  {ThresholdPRsPerDay: 1, WindowDays: 30},
			"alpha": {ThresholdPRsPerDay: 5, WindowDays: 30},
			"mid":   {ThresholdPRsPerDay: 10, WindowDays: 30},
		},
	}
	rows := buildRows(f, map[string]triggers.State{}, time.Now())
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].Name != "alpha" || rows[1].Name != "mid" || rows[2].Name != "zeta" {
		t.Fatalf("rows not sorted: %v", rows)
	}
}

// TestTriggers_BuildRowsStatusReflectsState pins status classification (#636).
func TestTriggers_BuildRowsStatusReflectsState(t *testing.T) {
	f := triggers.File{
		Triggers: map[string]triggers.TriggerSpec{
			"green-clock-10pd": {ThresholdPRsPerDay: 10, WindowDays: 30},
			"green-clock-5pd":  {ThresholdPRsPerDay: 5, WindowDays: 30},
			"green-clock-1pd":  {ThresholdPRsPerDay: 1, WindowDays: 30},
		},
	}
	states := map[string]triggers.State{
		"green-clock-10pd": {DayCount: 0, DaysRemaining: 30},
		"green-clock-5pd":  {DayCount: 12, DaysRemaining: 18},
		"green-clock-1pd":  {DayCount: 30, DaysRemaining: 0, WindowComplete: true},
	}
	rows := buildRows(f, states, time.Now())
	want := map[string]string{
		"green-clock-10pd": triggerStatusPending,
		"green-clock-5pd":  triggerStatusRunning,
		"green-clock-1pd":  triggerStatusComplete,
	}
	for _, r := range rows {
		if got := r.Status; got != want[r.Name] {
			t.Errorf("%s status = %q, want %q", r.Name, got, want[r.Name])
		}
	}
}

// TestTriggers_RenderEmptyShowsHint asserts the operator-readable empty-config hint (#636).
func TestTriggers_RenderEmptyShowsHint(t *testing.T) {
	out := renderTriggers(nil)
	if !strings.Contains(out, "no triggers configured") {
		t.Errorf("empty render missing hint: %q", out)
	}
}

// TestTriggers_RenderHeaderColumns asserts the table header line (#636).
func TestTriggers_RenderHeaderColumns(t *testing.T) {
	rows := []triggerRow{{
		Name: "n", Threshold: "10 PRs/day", Window: "30 days",
		DayCount: 0, DaysLeft: 30, Status: "pending", LastFired: "—",
	}}
	out := renderTriggers(rows)
	for _, want := range []string{"NAME", "THRESHOLD", "WINDOW", "DAY-COUNT", "REMAIN", "STATUS", "LAST-FIRED"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "10 PRs/day") || !strings.Contains(out, "pending") {
		t.Errorf("row not rendered: %s", out)
	}
}

// TestTriggers_EmitJSONOneLinePerRow pins NDJSON shape for scripted consumers (#636).
func TestTriggers_EmitJSONOneLinePerRow(t *testing.T) {
	rows := []triggerRow{
		{Name: "a", Threshold: "1 PRs/day", Window: "30 days", DayCount: 0, DaysLeft: 30, Status: "pending", LastFired: "—"},
		{Name: "b", Threshold: "5 PRs/day", Window: "30 days", DayCount: 3, DaysLeft: 27, Status: "running", LastFired: "—"},
	}
	var buf bytes.Buffer
	if code := emitTriggersJSON(&buf, rows); code != 0 {
		t.Fatalf("emitTriggersJSON exit = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSON lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"name":"a"`) || !strings.Contains(lines[1], `"name":"b"`) {
		t.Errorf("json shape drift: %q", buf.String())
	}
}

// TestTriggers_RunUnknownFlagFails asserts flag parse returns exit 2 (#636).
func TestTriggers_RunUnknownFlagFails(t *testing.T) {
	if code := runTriggers([]string{"--not-a-flag"}); code != 2 {
		t.Errorf("runTriggers unknown flag exit = %d, want 2", code)
	}
}

// TestTriggers_RunMissingConfigFails asserts missing config returns exit 1 (#636).
func TestTriggers_RunMissingConfigFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent.yaml")
	if code := runTriggers([]string{"--config", missing}); code != 1 {
		t.Errorf("runTriggers missing config exit = %d, want 1", code)
	}
}

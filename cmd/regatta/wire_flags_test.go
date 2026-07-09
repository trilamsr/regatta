package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
	"time"
)

// TestValidateServeFlags_RejectsNonPositiveDurations asserts --poll / --tick / --heartbeat / --lock-ttl reject 0 or negative (R16-Bug-1; orchestrator silently substitutes defaults today, hiding operator typos).
func TestValidateServeFlags_RejectsNonPositiveDurations(t *testing.T) {
	cases := []struct {
		name  string
		f     serveFlags
		field string
	}{
		{"poll_zero", serveFlags{PollDur: 0, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: time.Minute}, "--poll"},
		{"poll_negative", serveFlags{PollDur: -1, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: time.Minute}, "--poll"},
		{"tick_zero", serveFlags{PollDur: 30 * time.Second, TickDur: 0, HeartDur: time.Minute, LockTTL: time.Minute}, "--tick"},
		{"heart_negative", serveFlags{PollDur: 30 * time.Second, TickDur: 5 * time.Second, HeartDur: -1, LockTTL: time.Minute}, "--heartbeat"},
		{"lock_ttl_zero", serveFlags{PollDur: 30 * time.Second, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: 0}, "--lock-ttl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServeFlags(tc.f)
			if err == nil {
				t.Fatalf("want err mentioning %s; got nil", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("err = %q; want mention of %s", err, tc.field)
			}
		})
	}
}

// TestValidateServeFlags_AcceptsPositives asserts positive values pass.
func TestValidateServeFlags_AcceptsPositives(t *testing.T) {
	f := serveFlags{PollDur: 30 * time.Second, TickDur: 5 * time.Second, HeartDur: time.Minute, LockTTL: 15 * time.Minute}
	if err := validateServeFlags(f); err != nil {
		t.Fatalf("positive flags rejected: %v", err)
	}
}

// buildServeFlagSet mirrors the flag registrations in parseServeFlags so the
// tier-visibility tests can exercise printServeFlags without invoking the full
// yaml-resolution side-effects. Kept in sync with parseServeFlags — the flag
// LIST (not defaults) is what drives serveDefaultFlags membership under test.
func buildServeFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(subcmdServe, flag.ContinueOnError)
	var f serveFlags
	f.LaneCaps = laneCapsFlag{}
	f.LogFormat = logFormatFlag(defaultLogFormat)
	fs.StringVar(&f.DBPath, flagDB, "", "")
	fs.StringVar(&f.ItemsRoot, flagItemsRoot, ".", "")
	fs.BoolVar(&f.TickOnce, flagTickOnce, false, "")
	fs.DurationVar(&f.PollDur, flagPoll, 30*time.Second, "")
	fs.DurationVar(&f.TickDur, flagTick, 5*time.Second, "")
	fs.DurationVar(&f.HeartDur, flagHeartbeat, 60*time.Second, "")
	fs.DurationVar(&f.LockTTL, flagLockTTL, 15*time.Minute, "")
	fs.StringVar(&f.SpawnerName, flagSpawner, "stub", "")
	fs.StringVar(&f.RepoRoot, flagRepo, ".", "")
	fs.StringVar(&f.ClaudeBin, flagClaude, spawnerNameClaude, "")
	fs.StringVar(&f.BaseRef, flagBaseRef, "HEAD", "")
	fs.Var(f.LaneCaps, flagLane, "")
	fs.Var(&f.LogFormat, flagLogFormat, "")
	fs.StringVar(&f.Addr, flagAddr, "", "")
	fs.BoolVar(&f.UI, flagUI, true, "")
	fs.BoolVar(&f.NoPRWatch, flagNoPRWatch, false, "")
	fs.BoolVar(&f.AutoMerge, flagAutoMerge, false, "")
	fs.StringVar(&f.PublicURL, flagPublicURL, "", "")
	fs.Bool(flagAdvancedHelp, false, "")
	return fs
}

// hasFlagLine reports whether printServeFlags rendered a top-level `-<name>` entry.
// printServeFlags prefixes each flag with 2 spaces + dash, so per-line prefix
// match avoids false-positives (e.g. -tick vs -tick-once).
func hasFlagLine(rendered, name string) bool {
	needle := "  -" + name
	for _, line := range strings.Split(rendered, "\n") {
		if line == needle || strings.HasPrefix(line, needle+" ") {
			return true
		}
	}
	return false
}

// TestPrintServeFlags_DefaultTierHidesTuningKnobs asserts the default `-h` surface shows the 9 core flags and hides the 9 tuning knobs behind `-advanced-help` (W8 CLI UX tier gate, MED #1394).
func TestPrintServeFlags_DefaultTierHidesTuningKnobs(t *testing.T) {
	var buf bytes.Buffer
	fs := buildServeFlagSet()
	printServeFlags(&buf, fs, false)
	out := buf.String()

	visible := []string{flagAddr, flagDB, flagSpawner, flagUI, flagAutoMerge, flagPublicURL, flagRepo, flagLane, flagTickOnce, flagAdvancedHelp}
	hidden := []string{flagPoll, flagTick, flagHeartbeat, flagLockTTL, flagBaseRef, flagItemsRoot, flagClaude, flagLogFormat, flagNoPRWatch}

	for _, name := range visible {
		if !hasFlagLine(out, name) {
			t.Errorf("default tier missing core flag -%s\n---\n%s", name, out)
		}
	}
	for _, name := range hidden {
		if hasFlagLine(out, name) {
			t.Errorf("default tier leaked tuning flag -%s\n---\n%s", name, out)
		}
	}
	if !strings.Contains(out, "-advanced-help") {
		t.Errorf("default tier missing -advanced-help footer nudge\n---\n%s", out)
	}
}

// TestPrintServeFlags_AdvancedTierShowsAll asserts `-advanced-help` surfaces all 19 flags including tuning knobs (W8 CLI UX tier gate, MED #1394).
func TestPrintServeFlags_AdvancedTierShowsAll(t *testing.T) {
	var buf bytes.Buffer
	fs := buildServeFlagSet()
	printServeFlags(&buf, fs, true)
	out := buf.String()

	all := []string{
		flagAddr, flagDB, flagSpawner, flagUI, flagAutoMerge, flagPublicURL, flagRepo, flagLane, flagTickOnce,
		flagPoll, flagTick, flagHeartbeat, flagLockTTL, flagBaseRef, flagItemsRoot, flagClaude, flagLogFormat, flagNoPRWatch,
		flagAdvancedHelp,
	}
	for _, name := range all {
		if !hasFlagLine(out, name) {
			t.Errorf("advanced tier missing flag -%s\n---\n%s", name, out)
		}
	}
}

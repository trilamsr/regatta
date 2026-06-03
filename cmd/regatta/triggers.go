package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/triggers"
)

// subcmdTriggers binds the standalone triggers CLI verb (#636 — relocated
// from the status TUI's 6th panel per OBS-D spec §5).
const subcmdTriggers = "triggers"

// Trigger status enum — closed set. Mirrors the State classification
// the dashboard tile uses; kept here as constants so goconst is happy
// and a status rename moves through one diff.
const (
	triggerStatusPending  = "pending"
	triggerStatusRunning  = "running"
	triggerStatusComplete = "complete"
)

// triggerRow is one rendered row in the triggers table — derived from
// a triggers.File entry + the live State computed from substrate
// events. Pure data so the renderer can be tested independently.
type triggerRow struct {
	Name      string
	Threshold string
	Window    string
	DayCount  int
	DaysLeft  int
	Status    string
	LastFired string
}

// renderTriggers formats rows as a fixed-width table. Width is unused
// today — kept for the same compact-mode shape `regatta status` uses
// once the table grows wider than 80 cols.
func renderTriggers(rows []triggerRow) string {
	if len(rows) == 0 {
		return "no triggers configured — see slo/triggers.yaml\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-28s %-22s %-10s %-9s %-7s %-12s %s\n",
		"NAME", "THRESHOLD", "WINDOW", "DAY-COUNT", "REMAIN", "STATUS", "LAST-FIRED")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-28s %-22s %-10s %-9d %-7d %-12s %s\n",
			r.Name, r.Threshold, r.Window, r.DayCount, r.DaysLeft, r.Status, r.LastFired)
	}
	return b.String()
}

// buildRows projects a triggers.File + an in-memory state map into the
// rendered shape. Pure function so a test can drive it without a
// substrate. now feeds the LastFired delta so tests stay deterministic.
func buildRows(f triggers.File, states map[string]triggers.State, now time.Time) []triggerRow {
	names := make([]string, 0, len(f.Triggers))
	for name := range f.Triggers {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]triggerRow, 0, len(names))
	for _, name := range names {
		spec := f.Triggers[name]
		st := states[name]
		status := triggerStatusPending
		switch {
		case st.WindowComplete:
			status = triggerStatusComplete
		case st.DayCount > 0:
			status = triggerStatusRunning
		}
		lastFired := "—"
		if !st.LastReset.IsZero() {
			lastFired = fmt.Sprintf("reset %s ago", now.Sub(st.LastReset).Round(time.Hour))
		}
		rows = append(rows, triggerRow{
			Name:      name,
			Threshold: fmt.Sprintf("%d PRs/day", spec.ThresholdPRsPerDay),
			Window:    fmt.Sprintf("%d days", spec.WindowDays),
			DayCount:  st.DayCount,
			DaysLeft:  st.DaysRemaining,
			Status:    status,
			LastFired: lastFired,
		})
	}
	return rows
}

// runTriggers parses --config + --json flags and writes the trigger
// table to stdout. Operator-readable table is the default; --json is
// available for scripted consumption.
func runTriggers(args []string) int {
	fs := flag.NewFlagSet("triggers", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "slo/triggers.yaml", "triggers.yaml path")
	jsonOut := fs.Bool("json", false, "render JSON instead of table")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "regatta triggers: flag parse:", err)
		return 2
	}

	f, err := triggers.LoadFile(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta triggers:", err)
		return 1
	}

	// Live State requires substrate events; the standalone CLI does not
	// open the substrate DB to keep the verb O(ms). States stay zero-
	// valued so the table renders threshold + status="pending" rows.
	// Operators get the same wiring drift signal the dashboard would
	// surface, without needing regatta serve to be up.
	states := map[string]triggers.State{}
	rows := buildRows(f, states, time.Now().UTC())

	if *jsonOut {
		return emitTriggersJSON(os.Stdout, rows)
	}
	_, _ = io.WriteString(os.Stdout, renderTriggers(rows))
	return 0
}

// emitTriggersJSON writes one JSON object per row, newline-delimited
// (NDJSON) — keeps the verb pipe-friendly for `jq` filtering. Returns
// non-zero only on writer error.
func emitTriggersJSON(out io.Writer, rows []triggerRow) int {
	for _, r := range rows {
		_, err := fmt.Fprintf(out,
			`{"name":%q,"threshold":%q,"window":%q,"day_count":%d,"days_remaining":%d,"status":%q,"last_fired":%q}`+"\n",
			r.Name, r.Threshold, r.Window, r.DayCount, r.DaysLeft, r.Status, r.LastFired)
		if err != nil {
			fmt.Fprintln(os.Stderr, "regatta triggers: write:", err)
			return 1
		}
	}
	return 0
}

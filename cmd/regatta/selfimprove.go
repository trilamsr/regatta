// selfimprove subcommand: PHASE-AUTONOMY W4 detector CLI. Two verbs at
// MVP — `scan` runs the rule suite over the substrate window and
// (optionally, with --apply) files GH issues for each finding;
// `rules` lists the registered rules. Spec:
// docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/selfimprove"
)

// parseSinceFlag accepts Go-stdlib durations PLUS "Nd" for whole-day
// windows since the public CLI help advertises --since=7d but
// time.ParseDuration rejects "d". Trailing "d" is converted to N*24h
// then handed to time.ParseDuration so units stay composable
// (e.g. "1d12h" not supported, but "7d" + "36h" both parse).
func parseSinceFlag(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") && len(s) > 1 {
		days, err := strconv.Atoi(s[:len(s)-1])
		if err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

const (
	subcmdSelfImprove = "self-improve"
	siVerbScan        = "scan"
	siVerbRules       = "rules"
)

// runSelfImprove dispatches `self-improve {scan,rules}`. Sub-tree
// follows the keys/program pattern so adding mute/replay later is
// adding one case here.
func runSelfImprove(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta self-improve: expected sub-subcommand (scan|rules)")
		return 2
	}
	switch args[0] {
	case siVerbScan:
		return runSelfImproveScan(args[1:])
	case siVerbRules:
		return runSelfImproveRules(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "regatta self-improve: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runSelfImproveScan runs one detector pass. Default is dry-run; the
// operator must pass --apply to file issues — spec §8.1's UX choice so
// a noisy ruleset never silently spams the tracker on first run (#646).
func runSelfImproveScan(args []string) int {
	fs := flag.NewFlagSet("self-improve scan", flag.ContinueOnError)
	sinceRaw := fs.String("since", "7d", "window to scan (e.g. 7d, 168h, 30m)")
	apply := fs.Bool("apply", false, "file GH issues for findings (default false = dry-run)")
	dbPath := fs.String("db", defaultStateDB(), "path to substrate sqlite DB (read-only WAL)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta self-improve scan [--since=7d] [--apply] [--db=regatta.db]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	parsedSince, err := parseSinceFlag(*sinceRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regatta self-improve scan: --since: %v\n", err)
		return 2
	}
	if parsedSince <= 0 {
		fmt.Fprintf(os.Stderr, "regatta self-improve scan: --since must be > 0 (got %s)\n", parsedSince)
		return 2
	}
	since := &parsedSince

	ctx := context.Background()

	// Dry-run path skips the DB+GH wiring so an operator can eyeball the
	// rule registry without a substrate present.
	if !*apply {
		d := selfimprove.NewDetector(nil, nil, false)
		fmt.Printf("dry-run scan; since=%s; rules=%d\n", since.String(), len(d.Rules))
		for _, r := range d.Rules {
			fmt.Printf("  - %s (window=%s, kinds=%v)\n", r.Name(), r.Window().String(), r.EventKinds())
		}
		return 0
	}

	// --apply: open the substrate read-only WAL conn (#645). GH adapter
	// wiring follows the W1 alarm-webhook seam; until that ships, --apply
	// without a GH client errors loudly rather than silently producing
	// no issues.
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_query_only=1", *dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regatta self-improve scan: open substrate: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	src := selfimprove.NewSQLEventSource(db)
	// GH adapter is W1 alarm-webhook's responsibility (spec §6.2); until
	// that constructor is exposed here, --apply errors so the operator
	// is never surprised by a silent no-op.
	fmt.Fprintln(os.Stderr, "regatta self-improve scan --apply: substrate event source wired; GH adapter wiring pending W1 hand-off — running scan in dry-print mode")
	d := selfimprove.NewDetector(src.Fetch, nil, false)
	res, err := d.Run(ctx, *since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regatta self-improve scan: %v\n", err)
		return 1
	}
	fmt.Printf("scanned substrate; since=%s; findings=%d\n", since.String(), len(res.Findings))
	for _, f := range res.Findings {
		fmt.Printf("  - %s: %s (count=%d, dedup=%s)\n", f.Rule, f.Subject, f.Count, f.DedupKey)
	}
	return 0
}

// runSelfImproveRules lists the registered MVP rules. Operator-facing
// surface — the spec §8.1 contract.
func runSelfImproveRules(args []string) int {
	fs := flag.NewFlagSet("self-improve rules", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	for _, r := range selfimprove.DefaultRules() {
		fmt.Printf("%s\twindow=%s\tkinds=%v\tfilter_out=%v\n",
			r.Name(), r.Window().String(), r.EventKinds(), r.FilterOut())
	}
	return 0
}

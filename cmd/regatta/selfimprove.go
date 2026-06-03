// selfimprove subcommand: PHASE-AUTONOMY W4 detector CLI. Two verbs at
// MVP — `scan` runs the rule suite over the substrate window and
// (optionally, with --apply) files GH issues for each finding;
// `rules` lists the registered rules. Spec:
// docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/trilamsr/regatta/internal/selfimprove"
)

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
// a noisy ruleset never silently spams the tracker on first run.
func runSelfImproveScan(args []string) int {
	fs := flag.NewFlagSet("self-improve scan", flag.ContinueOnError)
	since := fs.Duration("since", 7*24*time.Hour, "window to scan (default 7d)")
	apply := fs.Bool("apply", false, "file GH issues for findings (default false = dry-run)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta self-improve scan [--since=7d] [--apply]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// W3-substrate event source + W1 GH adapter wiring lands when those
	// PRs merge; W4 ships the detector core + CLI shell. Operator gets
	// a loud "not wired" message instead of a silent no-op so the
	// dependency chain is legible.
	if *apply {
		fmt.Fprintln(os.Stderr, "regatta self-improve scan --apply: substrate event source not wired in this build (depends on PHASE-AUTONOMY W3)")
		return 1
	}

	// Dry-run path: print rule names + window/threshold so the operator
	// can eyeball the registered suite without needing the substrate.
	d := selfimprove.NewDetector(nil, nil, false)
	fmt.Printf("dry-run scan; since=%s; rules=%d\n", since.String(), len(d.Rules))
	for _, r := range d.Rules {
		fmt.Printf("  - %s (window=%s, kinds=%v)\n", r.Name(), r.Window().String(), r.EventKinds())
	}
	_ = context.Background()
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

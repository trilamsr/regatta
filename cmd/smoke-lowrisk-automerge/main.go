// smoke-lowrisk-automerge exercises lowrisk.NewGhFetcher() + lowrisk.NewGate()
// end-to-end against a real PR (MAY-270 §1, live PRFetcher smoke harness).
//
// Two modes:
//
//  1. Stub mode (default): no flags. Builds a hand-crafted doc-only PR
//     value-object + a load-bearing PR value-object, runs the classifier
//     directly, asserts (eligible=true) and (eligible=false). Always
//     re-runnable; no `gh` required.
//
//  2. Live mode: --pr-doc-only=N1 --pr-load-bearing=N2 invokes the real
//     NewGhFetcher() against the running gh CLI. Asserts the same
//     outcomes against actual PRs the operator pre-selects. Requires
//     `gh` on PATH + valid auth.
//
// Exit codes: 0 = pass, 1 = assertion failure, 2 = wiring/setup error
// (gh missing in live mode, etc.). Stderr carries one line per failure.
//
// Smoke is intentionally not a Go test — operators run it ad-hoc before
// flipping --auto-merge=true + low_risk_automerge.enabled=true.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/merge/lowrisk"
)

const (
	exitFail    = 1
	exitWiring  = 2
	smokeLOCCap = 50
	smokeSoak   = 15 * time.Minute
)

func main() {
	prDocOnly := flag.Int("pr-doc-only", 0, "live PR number expected to classify ELIGIBLE (doc-only, ≤50 LoC, soaked)")
	prLoadBearing := flag.Int("pr-load-bearing", 0, "live PR number expected to classify HELD (touches load-bearing path)")
	flag.Parse()

	ctx := context.Background()
	if *prDocOnly == 0 && *prLoadBearing == 0 {
		if err := stubSmoke(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitFail)
		}
		fmt.Println("smoke-lowrisk-automerge: stub-mode PASS (classifier eligible+held)")
		return
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, "smoke-lowrisk-automerge: gh not on PATH (live mode requires gh auth)")
		os.Exit(exitWiring)
	}
	if err := liveSmoke(ctx, *prDocOnly, *prLoadBearing); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitFail)
	}
	fmt.Println("smoke-lowrisk-automerge: live-mode PASS")
}

// stubSmoke runs the classifier directly against synthetic PR
// value-objects so the harness is re-runnable without `gh` / live PRs.
func stubSmoke(_ context.Context) error {
	now := time.Now()
	c := lowrisk.New(lowrisk.Config{
		LOCCap:     smokeLOCCap,
		HoldWindow: smokeSoak,
		Clock:      func() time.Time { return now },
	})
	docOnly := lowrisk.PR{
		ChangedPaths: []string{"docs/readme.md"},
		DiffLOC:      10,
		OpenedAt:     now.Add(-1 * time.Hour),
	}
	eligible, reason := c.Classify(docOnly)
	if !eligible || reason != lowrisk.ReasonEligible {
		return fmt.Errorf("stub doc-only: eligible=%v reason=%q; want true/eligible", eligible, reason)
	}
	loadBearing := lowrisk.PR{
		ChangedPaths: []string{"internal/orchestrator/foo.go"},
		DiffLOC:      1,
		OpenedAt:     now.Add(-24 * time.Hour),
	}
	eligible, reason = c.Classify(loadBearing)
	if eligible || reason != lowrisk.ReasonLoadBearing {
		return fmt.Errorf("stub load-bearing: eligible=%v reason=%q; want false/load_bearing_path", eligible, reason)
	}
	return nil
}

// liveSmoke wires the real NewGhFetcher() + NewGate() against operator-
// supplied PR numbers, asserting eligible/held outcomes match the
// pre-selected classification.
func liveSmoke(ctx context.Context, prDocOnly, prLoadBearing int) error {
	c := lowrisk.New(lowrisk.Config{
		LOCCap:     smokeLOCCap,
		HoldWindow: smokeSoak,
	})
	g := lowrisk.NewGate(c, lowrisk.NewGhFetcher())
	if prDocOnly > 0 {
		eligible, reason := g.Eligible(ctx, prDocOnly, "")
		if !eligible {
			return fmt.Errorf("live doc-only PR #%d: eligible=false reason=%q; want true/eligible", prDocOnly, reason)
		}
		fmt.Printf("smoke-lowrisk-automerge: live PR #%d eligible=true reason=%q\n", prDocOnly, reason)
	}
	if prLoadBearing > 0 {
		eligible, reason := g.Eligible(ctx, prLoadBearing, "")
		if eligible {
			return fmt.Errorf("live load-bearing PR #%d: eligible=true; want false (held)", prLoadBearing)
		}
		fmt.Printf("smoke-lowrisk-automerge: live PR #%d eligible=false reason=%q\n", prLoadBearing, reason)
	}
	return nil
}

//go:build integration_gh

// Integration coverage for issue #632: the standard unit tests parse
// canned `gh --version` fixtures via regex. A future gh release that
// changes the `--version` line shape (build-metadata reordering, an
// extra preamble, etc.) would silently break detection in prod.
// This file shells the real binary so the contract surfaces here
// first. Build-tag gated so it stays out of the standard CI run;
// the nightly job (or on-demand `go test -tags integration_gh ./...`)
// pulls it in.

package prwatch

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestGHCLIVersionProbe_RealBinary asserts a semver triple is detected from real `gh --version` (#632).
func TestGHCLIVersionProbe_RealBinary(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skipf("gh not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	probe := NewGHCLIVersionProbe()
	got, err := probe.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got == "" {
		t.Fatalf("Version returned empty string")
	}
	// VersionAtLeast against 0.0.0 surfaces the parsed triple via a
	// successful comparison; an error here means the regex did not
	// find a semver in the real output → the shape changed.
	ok, err := VersionAtLeast(got, "0.0.0")
	if err != nil {
		t.Fatalf("VersionAtLeast parse failed on real gh output %q: %v", got, err)
	}
	if !ok {
		t.Fatalf("real gh version %q did not satisfy 0.0.0 floor — parser regression", got)
	}
	// Sanity-check the floor MinGHVersion the prod code enforces so the
	// nightly job also catches a real-world gh that fell below the floor.
	ok, err = VersionAtLeast(got, MinGHVersion)
	if err != nil {
		t.Fatalf("VersionAtLeast against MinGHVersion: %v", err)
	}
	if !ok {
		t.Logf("WARN: installed gh %q is below MinGHVersion %s — upgrade gh", got, MinGHVersion)
	}
}

package lowrisk

import (
	"testing"
	"time"
)

// fixedClock returns a clock pinned to now so soak math is deterministic.
func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

// TestLowRiskClassifier_LoadBearingChangeFailsClassify asserts the
// load-bearing veto is FIRST and UNCONDITIONAL: a PR touching a
// load-bearing path is rejected with reason "load_bearing_path" even
// when every secondary signal (tiny diff, fully soaked) would otherwise
// pass. This is the core safety invariant of MAY-86.
func TestLowRiskClassifier_LoadBearingChangeFailsClassify(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	// Tiny, long-soaked diff — secondary signals all green — but it
	// touches a load-bearing path, so the veto must fire first.
	pr := PR{
		ChangedPaths: []string{"internal/orchestrator/foo.go"},
		DiffLOC:      1,
		OpenedAt:     now.Add(-24 * time.Hour),
	}
	eligible, reason := c.Classify(pr)
	if eligible {
		t.Fatalf("load-bearing PR classified eligible; want held")
	}
	if reason != "load_bearing_path" {
		t.Fatalf("reason=%q; want load_bearing_path", reason)
	}
}

// TestLowRiskClassifier_LoadBearingVetoBeatsEveryOtherFailure asserts the
// veto reason wins even when LOC ALSO exceeds the cap — proving the
// load-bearing branch runs BEFORE the LOC branch (ordering, not just
// presence).
func TestLowRiskClassifier_LoadBearingVetoBeatsEveryOtherFailure(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	pr := PR{
		ChangedPaths: []string{"cmd/regatta/serve.go", "docs/x.md"},
		DiffLOC:      9999,                    // over cap
		OpenedAt:     now,                     // not soaked
	}
	eligible, reason := c.Classify(pr)
	if eligible {
		t.Fatalf("eligible=true; want false")
	}
	if reason != "load_bearing_path" {
		t.Fatalf("reason=%q; want load_bearing_path (veto must precede loc/soak)", reason)
	}
}

// TestLowRiskClassifier_LOCOverCapHolds asserts a non-load-bearing PR
// whose diff exceeds the LOC cap is held with reason "loc_over_cap".
func TestLowRiskClassifier_LOCOverCapHolds(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	pr := PR{
		ChangedPaths: []string{"docs/readme.md"},
		DiffLOC:      51,
		OpenedAt:     now.Add(-1 * time.Hour),
	}
	eligible, reason := c.Classify(pr)
	if eligible {
		t.Fatalf("eligible=true; want false")
	}
	if reason != "loc_over_cap" {
		t.Fatalf("reason=%q; want loc_over_cap", reason)
	}
}

// TestLowRiskClassifier_SoakNotSatisfiedHolds asserts a PR younger than
// the hold window is held with reason "soak_not_satisfied" even when
// path + LOC are fine.
func TestLowRiskClassifier_SoakNotSatisfiedHolds(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	pr := PR{
		ChangedPaths: []string{"docs/readme.md"},
		DiffLOC:      10,
		OpenedAt:     now.Add(-5 * time.Minute), // < 15m hold window
	}
	eligible, reason := c.Classify(pr)
	if eligible {
		t.Fatalf("eligible=true; want false")
	}
	if reason != "soak_not_satisfied" {
		t.Fatalf("reason=%q; want soak_not_satisfied", reason)
	}
}

// TestLowRiskClassifier_HappyLowRiskDocsPREligible asserts a docs-only
// PR under the LOC cap and past the soak window is eligible with reason
// "eligible".
func TestLowRiskClassifier_HappyLowRiskDocsPREligible(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	pr := PR{
		ChangedPaths: []string{"docs/readme.md", "README.md"},
		DiffLOC:      12,
		OpenedAt:     now.Add(-20 * time.Minute), // soaked
	}
	eligible, reason := c.Classify(pr)
	if !eligible {
		t.Fatalf("eligible=false reason=%q; want eligible", reason)
	}
	if reason != "eligible" {
		t.Fatalf("reason=%q; want eligible", reason)
	}
}

// TestLowRiskClassifier_SoakBoundaryInclusive asserts the soak check is
// inclusive at exactly HoldWindow (>=), matching the stateless
// Clock().Sub(OpenedAt) >= HoldWindow contract.
func TestLowRiskClassifier_SoakBoundaryInclusive(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	pr := PR{
		ChangedPaths: []string{"docs/readme.md"},
		DiffLOC:      1,
		OpenedAt:     now.Add(-15 * time.Minute), // exactly at window
	}
	eligible, _ := c.Classify(pr)
	if !eligible {
		t.Fatalf("eligible=false at exact hold-window boundary; want inclusive >=")
	}
}

// TestLoadBearingPaths_EmbeddedListCoversT3Set asserts the embedded
// single-source-of-truth path list covers the broader spec-§3 T3 set so
// the embed-and-shell-read pair shares one physical file. Each prefix
// MUST veto.
func TestLoadBearingPaths_EmbeddedListCoversT3Set(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	t3 := []string{
		"internal/ghclient/x.go",
		"internal/gates/x.go",
		"internal/orchestrator/x.go",
		"internal/supervisor/x.go",
		"internal/ghidempotency/x.go",
		"internal/secrets/x.go",
		"internal/sandbox/x.go",
		"internal/web/x.go",
		"internal/obs/x.go",
		"internal/cost/x.go",
		"internal/canon/x.go",
		"cmd/regatta/main.go",
		"contracts/schemas/regatta.v1.cue",
		"Makefile",
		"Makefile.d/ci.mk",
		".github/workflows/ci.yml",
		"scripts/check-tdd.sh",
	}
	for _, p := range t3 {
		pr := PR{ChangedPaths: []string{p}, DiffLOC: 1, OpenedAt: now.Add(-24 * time.Hour)}
		eligible, reason := c.Classify(pr)
		if eligible || reason != "load_bearing_path" {
			t.Errorf("path %q: eligible=%v reason=%q; want held load_bearing_path", p, eligible, reason)
		}
	}
}

// TestLoadBearingPaths_NonCheckScriptNotVetoed asserts the scripts veto
// is scoped to scripts/check-*.sh (CI gates), not every script — a
// non-check script under scripts/ is NOT load-bearing for this gate.
func TestLoadBearingPaths_NonCheckScriptNotVetoed(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := New(Config{LOCCap: 50, HoldWindow: 15 * time.Minute, Clock: fixedClock(now)})

	pr := PR{ChangedPaths: []string{"scripts/smoke-demo.sh"}, DiffLOC: 3, OpenedAt: now.Add(-1 * time.Hour)}
	eligible, reason := c.Classify(pr)
	if !eligible {
		t.Fatalf("non-check script held: reason=%q; want eligible", reason)
	}
}

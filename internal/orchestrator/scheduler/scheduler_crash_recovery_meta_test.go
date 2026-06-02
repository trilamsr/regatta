package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestCrashRecoveryRunner_DetectsForcedDivergence pins the diff harness — if recovery skips work, diffSnapshots must fire.
func TestCrashRecoveryRunner_DetectsForcedDivergence(t *testing.T) {
	h := &crashHarness{
		seedIDs: []string{"WI-A", "WI-B"},
		lane:    "server",
		lockTTL: time.Minute,
	}
	baseline, writes := h.runBaseline(t, 2)
	if writes[0] == 0 {
		t.Fatalf("baseline tick[0] has no writes — cannot exercise crash semantics")
	}

	// Skip the recovery tick entirely — emulate a bug where the recovered
	// process forgets to re-tick. The forced-divergence must show up as
	// agent-state drift between baseline (spawned) and "recovered" (still
	// pending or absent).
	forcedDB := openCrashDB(t)
	h.seedQ(t, forcedDB)
	if d := diffSnapshots(baseline, snapshotState(t, context.Background(), forcedDB)); d == "" {
		t.Fatalf("diffSnapshots did not flag forced divergence — runner is blind to recovery skips")
	} else if !strings.Contains(d, "agent[WI-A] missing") || !strings.Contains(d, "agent[WI-B] missing") {
		t.Fatalf("forced-divergence diff missing expected agent labels: %s", d)
	}
}

// TestCrashRecoveryRunner_BaselineMatchesRecover pins the happy path — crash at first write of tick 0 then recover.
func TestCrashRecoveryRunner_BaselineMatchesRecover(t *testing.T) {
	h := &crashHarness{
		seedIDs: []string{"WI-A"},
		lane:    "server",
		lockTTL: time.Minute,
	}
	baseline, _ := h.runBaseline(t, 2)
	recovered := h.runCrashAndRecover(t, 2, 0, 0)
	if d := diffSnapshots(baseline, recovered); d != "" {
		t.Fatalf("baseline ≠ recovered for crash-at-first-write of empty Q: %s", d)
	}
	if a, ok := recovered.Agents["WI-A"]; !ok || a.State != state.AgentSpawning {
		t.Fatalf("WI-A post-recover want spawning got %+v", a)
	}
}

// TestCrashRecoveryRunner_CatchesMissingRecoveryTick pins the TDD invariant — diff must catch a missing post-crash tick.
func TestCrashRecoveryRunner_CatchesMissingRecoveryTick(t *testing.T) {
	h := &crashHarness{
		seedIDs: []string{"WI-A"},
		lane:    "server",
		lockTTL: time.Minute,
	}
	baseline, _ := h.runBaseline(t, 2)

	// Simulate the bug: run the crash tick, do NOT spawn the recovery scheduler.
	db := openCrashDB(t)
	h.seedQ(t, db)
	crash := New(db, Config{LockTTL: h.lockTTL, Hotspots: h.hotspot})
	crash.WriteHook = func(idx int) error {
		if idx == 0 {
			return errCrashSim
		}
		return nil
	}
	_, _ = crash.Tick(context.Background())
	// Bug: skip the recovery scheduler + remaining ticks.
	buggy := snapshotState(t, context.Background(), db)
	if d := diffSnapshots(baseline, buggy); d == "" {
		t.Fatalf("diff did not catch missing recovery — runner blind to the canonical bug")
	}
}

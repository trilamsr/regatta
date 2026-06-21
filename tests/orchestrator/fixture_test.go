//go:build unix

// Package orchestrator_test exercises the cross-package integration
// harness (MAY-49). The package import name is `orchestrator_test` to
// avoid colliding with internal/orchestrator while still living under
// tests/orchestrator/ on disk.
package orchestrator_test

import (
	"context"
	"testing"

	testharness "github.com/trilamsr/regatta/tests/orchestrator"
)

// TestBootOrchestrator_ReturnsWiredHarness asserts the fixture returns a Harness whose spawner+scheduler+state seams are wired and observable (MAY-49 AC1+AC2).
func TestBootOrchestrator_ReturnsWiredHarness(t *testing.T) {
	h := testharness.BootOrchestrator(t)
	if h == nil {
		t.Fatal("BootOrchestrator returned nil")
	}
	if h.DB == nil {
		t.Fatal("Harness.DB nil")
	}
	if h.Spawner == nil {
		t.Fatal("Harness.Spawner nil (real spawner.Stub seam required)")
	}
	if h.Scheduler == nil {
		t.Fatal("Harness.Scheduler nil (real scheduler.Scheduler seam required)")
	}
	if h.Orchestrator == nil {
		t.Fatal("Harness.Orchestrator nil")
	}
	if h.Lister == nil {
		t.Fatal("Harness.Lister nil (shared in-memory prwatch.PRLister required)")
	}

	// Smoke: PollOnce + ScheduleOnce must run without error on an empty
	// adapter root — the harness must wire enough seams that the
	// orchestrator's tick loop is callable.
	ctx := context.Background()
	if err := h.Orchestrator.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce on empty harness: %v", err)
	}
	if err := h.Orchestrator.ScheduleOnce(ctx); err != nil {
		t.Fatalf("ScheduleOnce on empty harness: %v", err)
	}
}

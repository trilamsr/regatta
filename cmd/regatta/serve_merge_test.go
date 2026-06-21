// serve_merge_test pins the buildMergeWiring boot contract — the
// gh-version gate (#656) and the worker-disabled path. Heavier
// end-to-end coverage lives in serve_claude_test.go; this file owns
// the boot-refusal contract specifically.
package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/merge/lowrisk"
)

// TestBuildMergeWiring_GhVersionTooOld_RefusesInit asserts a gh 2.39 environment refuses worker init at boot (#656).
func TestBuildMergeWiring_GhVersionTooOld_RefusesInit(t *testing.T) {
	db := openSchedulerTestDB(t)
	// Stub the gh-version seam: fake a 2.39 install. Restore after so
	// later tests in the package keep the real probe.
	prev := verifyGhVersionFn
	t.Cleanup(func() { verifyGhVersionFn = prev })
	verifyGhVersionFn = func(_ context.Context, _ *slog.Logger) error {
		return merge.ErrGhVersionUnsupported
	}

	coord, worker, _, err := buildMergeWiring(db, t.TempDir(), true, slog.Default())
	if err == nil {
		t.Fatalf("buildMergeWiring returned nil err for gh 2.39, want refusal")
	}
	if !errors.Is(err, merge.ErrGhVersionUnsupported) {
		t.Fatalf("err=%v, want ErrGhVersionUnsupported wrap", err)
	}
	if coord != nil || worker != nil {
		t.Fatalf("expected nil coord+worker on refusal, got coord=%v worker=%v", coord, worker)
	}
}

// TestBuildMergeWiring_AutoMergeDisabled_SkipsVersionCheck asserts the gh-version gate only fires when the worker is enabled — operators running without --auto-merge get the Reconcile-only path even on old gh.
func TestBuildMergeWiring_AutoMergeDisabled_SkipsVersionCheck(t *testing.T) {
	db := openSchedulerTestDB(t)
	called := false
	prev := verifyGhVersionFn
	t.Cleanup(func() { verifyGhVersionFn = prev })
	verifyGhVersionFn = func(_ context.Context, _ *slog.Logger) error {
		called = true
		return merge.ErrGhVersionUnsupported
	}

	coord, worker, gate, err := buildMergeWiring(db, t.TempDir(), false, slog.Default())
	if err != nil {
		t.Fatalf("buildMergeWiring err=%v with auto-merge off; version gate should not fire", err)
	}
	if coord == nil {
		t.Fatalf("coord nil; Reconcile path must stay live regardless of --auto-merge")
	}
	if worker != nil {
		t.Fatalf("worker non-nil with --auto-merge=false")
	}
	if gate != nil {
		t.Fatalf("low-risk gate non-nil with --auto-merge=false; want nil (no-op path)")
	}
	if called {
		t.Fatalf("version probe called with --auto-merge=false; gate must be worker-gated")
	}
}

// TestBuildMergeWiring_GhVersionOk_BuildsWorker asserts a gh 2.55 install builds both coordinator + worker.
func TestBuildMergeWiring_GhVersionOk_BuildsWorker(t *testing.T) {
	db := openSchedulerTestDB(t)
	prev := verifyGhVersionFn
	t.Cleanup(func() { verifyGhVersionFn = prev })
	verifyGhVersionFn = func(_ context.Context, _ *slog.Logger) error { return nil }

	coord, worker, gate, err := buildMergeWiring(db, t.TempDir(), true, slog.Default())
	if err != nil {
		t.Fatalf("buildMergeWiring: %v", err)
	}
	if coord == nil || worker == nil {
		t.Fatalf("expected coord+worker; got coord=%v worker=%v", coord, worker)
	}
	// --auto-merge=true with no regatta.yaml → conservative-default HoldAll.
	if _, ok := gate.(lowrisk.HoldAll); !ok {
		t.Fatalf("conservative default must wire HoldAll; got %T", gate)
	}
}

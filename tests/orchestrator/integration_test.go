//go:build unix

// integration_test.go demonstrates cross-package coverage built on the
// BootOrchestrator fixture (MAY-49). Two migrated patterns:
//
//   - TestHarness_SurvivesFailingAdapter — cross-package mirror of the
//     #1156 timing contract pinned in
//     internal/orchestrator/orchestrator_test.go::TestRunSurvivesFailingAdapter.
//     The original lives in package orchestrator and reaches into the
//     internal newHarness; this version exercises the same contract
//     through the public BootOrchestrator surface so a regression in
//     either layer fails here too.
//
//   - TestHarness_IntakeToSpawnToMerge — cross-package mirror of the
//     tests/selfhost_smoke_test.go A2+A3+A4 acceptance chain (adapter →
//     scheduler reserve → spawner.Stub spawn → Complete writes journal
//     + flips merged). Asserts the same invariant chain through the
//     BootOrchestrator-wired seams.
package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	internalorch "github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil"
	testharness "github.com/trilamsr/regatta/tests/orchestrator"
)

// failingAdapter mirrors the original orchestrator_test failingAdapter; every List returns an error so Run must keep ticking.
type failingAdapter struct{ calls atomic.Int64 }

func (a *failingAdapter) List(context.Context) ([]schemas.WorkItem, error) {
	n := a.calls.Add(1)
	return nil, fmt.Errorf("synthetic adapter failure %d", n)
}
func (a *failingAdapter) Get(context.Context, schemas.WorkItemID) (schemas.WorkItem, error) {
	return schemas.WorkItem{}, schemas.ErrNotFound
}
func (a *failingAdapter) UpdateStatus(context.Context, schemas.WorkItemID, schemas.Status, string) error {
	return nil
}
func (a *failingAdapter) Capabilities() schemas.Capabilities {
	return schemas.Capabilities{MinPollInterval: time.Millisecond}
}

// TestHarness_SurvivesFailingAdapter mirrors the #1156 contract via BootOrchestrator: Run must survive every-tick adapter failures and shut down cleanly.
func TestHarness_SurvivesFailingAdapter(t *testing.T) {
	// The harness wires a markdown adapter; for this case we need a
	// failing adapter, so we re-boot the orchestrator with the harness
	// DB + spawner but a hostile adaptersync. The harness's role here
	// is "give me a hermetic DB+spawner+scheduler in one call"; the
	// boot-with-custom-adapter path stays composable.
	h := testharness.BootOrchestrator(t)

	ad := &failingAdapter{}
	syncer, err := adaptersync.New(adaptersync.Config{Adapter: ad, DB: h.DB})
	if err != nil {
		t.Fatalf("adaptersync.New: %v", err)
	}
	o := internalorch.New(internalorch.Config{
		AdapterSync:       syncer,
		BriefLoader:       noopLoader{},
		DB:                h.DB,
		Scheduler:         h.Scheduler,
		Spawner:           h.Spawner,
		DBPath:            h.DBPath,
		PollInterval:      10 * time.Millisecond,
		TickInterval:      10 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
		LockTTL:           time.Minute,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 5*time.Millisecond, func() bool {
		return ad.calls.Load() >= 2
	}, "adapter polled <2 times despite failures (#1156 timing)")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v for failing adapter; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// TestHarness_IntakeToSpawnToMerge mirrors the selfhost smoke test A2+A3+A4 chain through BootOrchestrator's spawner+scheduler+state seams.
func TestHarness_IntakeToSpawnToMerge(t *testing.T) {
	ctx := context.Background()
	h := testharness.BootOrchestrator(t)

	const wantID = "HARNESS-1"
	writeItem(t, h.AdapterRoot, wantID)

	// Re-boot the syncer against the harness AdapterRoot — BootOrchestrator
	// already wired a markdown adapter, so PollOnce will pick up the
	// fixture we just wrote.
	if err := h.Orchestrator.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	wi, err := h.DB.GetWorkItem(ctx, wantID)
	if err != nil {
		t.Fatalf("GetWorkItem after PollOnce: %v", err)
	}
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("status=%q; want %q after PollOnce", wi.Status, state.WorkStatusPlanned)
	}

	if err := h.Orchestrator.ScheduleOnce(ctx); err != nil {
		t.Fatalf("ScheduleOnce: %v", err)
	}
	calls := h.Spawner.Calls()
	if len(calls) != 1 || calls[0].WorkItemID != wantID {
		t.Fatalf("spawner calls=%+v; want [{WorkItemID:%s}]", calls, wantID)
	}

	const prURL = "https://github.com/trilamsr/regatta/pull/HARNESS"
	if err := h.Spawner.Complete(ctx, wantID, json.RawMessage(`{"pr_url":"`+prURL+`","completed":true}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	out, err := h.DB.GetLatestOutput(ctx, wantID)
	if err != nil {
		t.Fatalf("GetLatestOutput: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out.OutputJSON), &parsed); err != nil {
		t.Fatalf("journal not JSON: %v (%q)", err, out.OutputJSON)
	}
	if parsed["pr_url"] != prURL {
		t.Fatalf("journal pr_url=%v; want %q", parsed["pr_url"], prURL)
	}
	final, err := h.DB.GetWorkItem(ctx, wantID)
	if err != nil {
		t.Fatalf("GetWorkItem final: %v", err)
	}
	if final.Status != state.WorkStatusMerged {
		t.Fatalf("final status=%q; want %q", final.Status, state.WorkStatusMerged)
	}
}

// writeItem writes a minimal markdown brief the markdown catalog adapter parses.
func writeItem(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, ".regatta", "items")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(fmt.Sprintf(`---
id: %s
title: %s
kind: feature
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: only criterion
`, id, id))
	if err := os.WriteFile(filepath.Join(dir, id+".md"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

type noopLoader struct{}

func (noopLoader) Sync(context.Context, time.Time) error { return nil }

// Compile-time unused-import guard: keep adapter+scheduler+spawner
// references live so a refactor that breaks the harness signature
// fails the migrated tests rather than silently dropping coverage.
var _ = adapter.NewMarkdownCatalog
var _ scheduler.Config
var _ spawner.Stub

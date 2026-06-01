//go:build unix

// PollOnce's lockfile contract is POSIX-only (see
// lockfile/lockfile_test.go header). Excluded from Windows builds —
// orchestrator runtime targets Linux + macOS only.

package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/lockfile"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
)

// okAdapter is the empty-list adapter used by the happy-path PollOnce
// test: nothing for adaptersync to mirror, briefLoader is no-op on an
// empty FS. PollOnce must still return nil.
type okAdapter struct{}

func (okAdapter) List(context.Context) ([]schemas.WorkItem, error) {
	return nil, nil
}

// listFailingAdapter fails on every List so we can prove the orchestrator
// fail-fasts when adaptersync.Sync errors. Distinct from
// orchestrator_test.go's failingAdapter so the rewire of PollOnce can
// be tested independently of the legacy Run-survives-failure test.
type listFailingAdapter struct{}

func (listFailingAdapter) List(context.Context) ([]schemas.WorkItem, error) {
	return nil, fmt.Errorf("synthetic adapter list failure")
}

// newPollOnceHarness wires the new Config-based Orchestrator with an
// empty briefs FS so PollOnce exercises the flock -> AdapterSync ->
// BriefLoader chain. Scheduler is included so the Config's required-
// field invariant holds even though PollOnce no longer calls Tick.
func newPollOnceHarness(t *testing.T, ad adaptersync.SpecAdapter) (*orchestrator.Orchestrator, *state.DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	syncer := adaptersync.New(adaptersync.Config{Adapter: ad, DB: db})
	loader := program.NewBriefLoader(program.BriefLoaderConfig{FS: fstest.MapFS{}, DB: db, Keyring: map[string][]byte{}})
	sched := scheduler.New(db, scheduler.Config{})
	o := orchestrator.New(orchestrator.Config{
		AdapterSync: syncer,
		BriefLoader: loader,
		DB:          db,
		Scheduler:   sched,
		Spawner:     spawner.New(spawner.Config{}),
		DBPath:      dbPath,
	})
	return o, db, dbPath
}

func TestPollOnce_FlockHeldReturnsError(t *testing.T) {
	o, _, dbPath := newPollOnceHarness(t, okAdapter{})

	// Pre-acquire the lockfile so PollOnce's flock attempt loses.
	lock1, err := lockfile.Acquire(dbPath + ".lock")
	if err != nil {
		t.Fatalf("pre-acquire lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	err = o.PollOnce(context.Background())
	if !errors.Is(err, orchestrator.ErrFlockHeld) {
		t.Fatalf("PollOnce err=%v want ErrFlockHeld", err)
	}
}

func TestPollOnce_AdapterSyncFailReturnsEarly(t *testing.T) {
	o, db, _ := newPollOnceHarness(t, listFailingAdapter{})

	err := o.PollOnce(context.Background())
	if err == nil {
		t.Fatal("expected adapter-fail error, got nil")
	}
	if !strings.Contains(err.Error(), "adapter list") {
		t.Fatalf("err=%v expected to wrap adapter-list failure", err)
	}

	// Fail-fast contract: briefLoader.Sync must not have run after the
	// adapter-list failure; downstream materialization stays untouched.
	agents, err := db.ListAgentsByState(context.Background(),
		state.AgentPending, state.AgentSpawning, state.AgentRunning)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("adapter-fail did not fail-fast: %d agent rows present", len(agents))
	}
}

func TestPollOnce_HappyPathReturnsNoError(t *testing.T) {
	o, _, _ := newPollOnceHarness(t, okAdapter{})
	if err := o.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce on empty inputs returned %v, want nil", err)
	}
}

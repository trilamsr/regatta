package reaper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

type fakeKiller struct {
	mu      sync.Mutex
	signal  map[int64]int
	fail    error
}

func (f *fakeKiller) KillAgent(id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.signal == nil {
		f.signal = map[int64]int{}
	}
	f.signal[id]++
	if f.fail != nil {
		return false, f.fail
	}
	return true, nil
}

func newDB(t *testing.T) *state.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		filepath.Join(t.TempDir(), "state.db"))
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newWM(t *testing.T) *spawner.WorktreeManager {
	t.Helper()
	dir := t.TempDir()
	fake := &fakeRunner{}
	wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: dir, Runner: fake.run})
	if err != nil {
		t.Fatalf("wm: %v", err)
	}
	return wm
}

// fakeRunner is a local copy of the spawner test fake; importing
// across package internals would couple test code to non-exported
// symbols.
type fakeRunner struct{ mu sync.Mutex }

func (f *fakeRunner) run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(args) >= 4 && args[0] == "worktree" && args[1] == "add" {
		path := args[len(args)-2]
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, err
		}
	}
	if len(args) >= 4 && args[0] == "worktree" && args[1] == "remove" {
		path := args[len(args)-1]
		_ = os.RemoveAll(path)
	}
	return nil, nil
}

func mustTransition(t *testing.T, db *state.DB, id int64, to state.AgentState) {
	t.Helper()
	if _, err := db.TransitionAgent(context.Background(), id, to, state.AgentMutation{}); err != nil {
		t.Fatalf("transition %d → %s: %v", id, to, err)
	}
}

func upsert(t *testing.T, db *state.DB, workItem, lane string) *state.Agent {
	t.Helper()
	a, err := db.UpsertPending(context.Background(), workItem, lane)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return a
}

func driveToDone(t *testing.T, db *state.DB, id int64) {
	t.Helper()
	mustTransition(t, db, id, state.AgentSpawning)
	mustTransition(t, db, id, state.AgentRunning)
	mustTransition(t, db, id, state.AgentPROpen)
	mustTransition(t, db, id, state.AgentGatesRunning)
	mustTransition(t, db, id, state.AgentAwaitingMerge)
	mustTransition(t, db, id, state.AgentDone)
}

func TestReapIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	wm := newWM(t)
	killer := &fakeKiller{}
	r := New(db, wm, killer)

	a := upsert(t, db, "WORK-1", "server")
	if _, err := wm.Create(ctx, a.ID, "HEAD"); err != nil {
		t.Fatalf("create worktree: %v", err)
	}

	if err := r.Reap(ctx, a.ID); err != nil {
		t.Fatalf("reap 1: %v", err)
	}
	if err := r.Reap(ctx, a.ID); err != nil {
		t.Fatalf("reap 2: %v", err)
	}
	if _, statErr := os.Stat(wm.PathFor(a.ID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("worktree not removed: %v", statErr)
	}
	if killer.signal[a.ID] != 2 {
		t.Fatalf("expected 2 kill signals (one per Reap), got %d", killer.signal[a.ID])
	}
}

func TestReapAllSweepsTerminalAgents(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	wm := newWM(t)
	r := New(db, wm, nil)

	a := upsert(t, db, "WORK-1", "server")
	b := upsert(t, db, "WORK-2", "server")
	c := upsert(t, db, "WORK-3", "server") // stays pending - not reaped

	if _, err := wm.Create(ctx, a.ID, "HEAD"); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := wm.Create(ctx, b.ID, "HEAD"); err != nil {
		t.Fatalf("create b: %v", err)
	}

	driveToDone(t, db, a.ID)
	mustTransition(t, db, b.ID, state.AgentWithdrawn)

	if err := r.ReapAll(ctx); err != nil {
		t.Fatalf("reap all: %v", err)
	}
	if _, statErr := os.Stat(wm.PathFor(a.ID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a worktree not reaped")
	}
	if _, statErr := os.Stat(wm.PathFor(b.ID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("b worktree not reaped")
	}
	if _, err := wm.Create(ctx, c.ID, "HEAD"); err != nil {
		// c had no worktree yet - this just proves the path is free.
		t.Fatalf("c worktree path not free: %v", err)
	}
}

func TestReapAllAggregatesErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	wm := newWM(t)
	killer := &fakeKiller{fail: errors.New("synthetic")}
	r := New(db, wm, killer)

	a := upsert(t, db, "WORK-1", "server")
	driveToDone(t, db, a.ID)

	err := r.ReapAll(ctx)
	if err == nil {
		t.Fatal("expected aggregated error from failing killer")
	}
}

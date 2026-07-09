package reaper

import (
	"context"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// driveToCrashedWithPID mirrors the R19-A O3 stamp path: pending → spawning
// (with PID + SessionID) → crashed (PID + SessionID retained per the stamp).
func driveToCrashedWithPID(t *testing.T, db *state.DB, id int64, pid int, sess string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.TransitionAgent(ctx, id, state.AgentSpawning, state.AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
		t.Fatalf("→ spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, id, state.AgentCrashed, state.AgentMutation{}); err != nil {
		t.Fatalf("→ crashed: %v", err)
	}
}

// driveToCrashedNoPID models a rollbackReservation outcome — spawning began
// but the child PID was never stamped before the transition to crashed.
func driveToCrashedNoPID(t *testing.T, db *state.DB, id int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.TransitionAgent(ctx, id, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("→ spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, id, state.AgentCrashed, state.AgentMutation{}); err != nil {
		t.Fatalf("→ crashed: %v", err)
	}
}

func countEvents(t *testing.T, db *state.DB, kind string) int {
	t.Helper()
	evs, err := db.ListEvents(context.Background(), 1000)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestSweepCrashedWithPID_KillsStampedPIDAndRequeues(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	wm := newWM(t)
	killer := &fakeKiller{}
	r := New(Config{DB: db, WM: wm, Killer: killer.KillAgent})

	a := upsert(t, db, "WORK-1", "server")
	driveToCrashedWithPID(t, db, a.ID, 12345, "sess-1")

	if err := r.SweepCrashedWithPID(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if killer.signal[a.ID] != 1 {
		t.Fatalf("want 1 kill signal for stamped PID, got %d", killer.signal[a.ID])
	}

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentPending {
		t.Fatalf("want post-sweep state pending, got %s", got.State)
	}
	if got.PID != 0 {
		t.Fatalf("want PID cleared, got %d", got.PID)
	}
	if got.SessionID != "" {
		t.Fatalf("want SessionID cleared, got %q", got.SessionID)
	}

	if n := countEvents(t, db, string(obs.EventReapCrashedRequeued)); n != 1 {
		t.Fatalf("want 1 reaper.crashed_requeued event, got %d", n)
	}
}

func TestSweepCrashedWithPID_SkipsCrashedWithoutPID(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	wm := newWM(t)
	killer := &fakeKiller{}
	r := New(Config{DB: db, WM: wm, Killer: killer.KillAgent})

	a := upsert(t, db, "WORK-1", "server")
	driveToCrashedNoPID(t, db, a.ID)

	if err := r.SweepCrashedWithPID(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if killer.signal[a.ID] != 0 {
		t.Fatalf("clean-rollback crashed (PID=0) must not be killed; got %d signals", killer.signal[a.ID])
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentCrashed {
		t.Fatalf("clean-rollback crashed must be left in crashed; got %s", got.State)
	}
	if n := countEvents(t, db, string(obs.EventReapCrashedRequeued)); n != 0 {
		t.Fatalf("want 0 requeued events for PID=0 row, got %d", n)
	}
}

// errOnFirstKiller fails the first kill, succeeds on later kills, so the
// sweep is forced to continue past a per-agent error.
type errOnFirstKiller struct {
	called map[int64]int
}

func (e *errOnFirstKiller) KillAgent(id int64) (bool, error) {
	if e.called == nil {
		e.called = map[int64]int{}
	}
	e.called[id]++
	if len(e.called) == 1 && e.called[id] == 1 {
		return false, errors.New("synthetic kill failure")
	}
	return true, nil
}

func TestSweepCrashedWithPID_ContinuesOnError(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	wm := newWM(t)
	killer := &errOnFirstKiller{}
	r := New(Config{DB: db, WM: wm, Killer: killer.KillAgent})

	a := upsert(t, db, "WORK-1", "server")
	b := upsert(t, db, "WORK-2", "server")
	driveToCrashedWithPID(t, db, a.ID, 11111, "sess-a")
	driveToCrashedWithPID(t, db, b.ID, 22222, "sess-b")

	if err := r.SweepCrashedWithPID(ctx); err != nil {
		t.Fatalf("sweep must not abort on per-agent error: %v", err)
	}

	if killer.called[a.ID] != 1 || killer.called[b.ID] != 1 {
		t.Fatalf("want both agents to have KillAgent invoked (a=%d, b=%d)",
			killer.called[a.ID], killer.called[b.ID])
	}

	gotB, err := db.GetAgent(ctx, b.ID)
	if err != nil {
		t.Fatalf("get agent b: %v", err)
	}
	if gotB.State != state.AgentPending {
		t.Fatalf("second crashed agent must still be requeued; got %s", gotB.State)
	}
}

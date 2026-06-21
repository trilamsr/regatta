//go:build unix

package orchestrator

import (
	"context"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// failingSpawner is declared in adversarial_test.go (same package).

// TestSpawnBackoff_SuppressesAfterFailure asserts a work_item that
// fails to spawn is not re-dispatched on the immediately following
// tick — the exponential cooldown gates re-admission (MAY-80).
func TestSpawnBackoff_SuppressesAfterFailure(t *testing.T) {
	b := newSpawnBackoff()
	const id = "wi-1"
	if !b.Admit(id) {
		t.Fatalf("first Admit=false; want true (no prior failure)")
	}
	b.RecordFailure(id)
	if b.Admit(id) {
		t.Fatalf("Admit=true on tick immediately after failure; want false (in cooldown)")
	}
}

// TestSpawnBackoff_ExponentialGrowth asserts consecutive failures widen
// the cooldown window: failure N suppresses for base<<(N-1) ticks, so
// the second failure's window is strictly longer than the first (MAY-80).
func TestSpawnBackoff_ExponentialGrowth(t *testing.T) {
	b := newSpawnBackoff()
	const id = "wi-1"

	window := func() int {
		n := 0
		for !b.Admit(id) {
			b.Tick()
			n++
			if n > 1024 {
				t.Fatalf("cooldown never elapsed; runaway window")
			}
		}
		return n
	}

	b.RecordFailure(id) // failure #1
	w1 := window()
	b.RecordFailure(id) // failure #2 (consecutive, no success between)
	w2 := window()
	if w2 <= w1 {
		t.Fatalf("second cooldown window=%d not > first=%d; backoff is not exponential", w2, w1)
	}
}

// TestSpawnBackoff_SuccessResetsWindow asserts RecordSuccess clears the
// failure count so the next cooldown is back to the base window (MAY-80).
func TestSpawnBackoff_SuccessResetsWindow(t *testing.T) {
	b := newSpawnBackoff()
	const id = "wi-1"
	for i := 0; i < 3; i++ {
		b.RecordFailure(id)
		for !b.Admit(id) {
			b.Tick()
		}
	}
	b.RecordSuccess(id)
	if !b.Admit(id) {
		t.Fatalf("Admit=false immediately after RecordSuccess; want true (state cleared)")
	}
}

// TestSpawnBackoff_PerWorkItemIsolation asserts one item's cooldown does
// not suppress a sibling item (MAY-80).
func TestSpawnBackoff_PerWorkItemIsolation(t *testing.T) {
	b := newSpawnBackoff()
	b.RecordFailure("wi-a")
	if b.Admit("wi-a") {
		t.Fatalf("wi-a Admit=true while in cooldown; want false")
	}
	if !b.Admit("wi-b") {
		t.Fatalf("wi-b Admit=false; want true (independent work item)")
	}
}

// TestScheduleOnce_SpawnFailureBacksOff asserts a second tick does NOT
// re-attempt the spawn of a work_item that failed on the first tick:
// the backoff suppresses re-dispatch and emits spawn.backoff_skipped
// instead of hammering the spawner every 5s (MAY-80).
func TestScheduleOnce_SpawnFailureBacksOff(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 1)
	fail := &failingSpawner{}
	o.spawner = fail
	h := obstest.New()
	o.log = slog.New(h)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule tick 1: %v", err)
	}
	if got := fail.calls.Load(); got != 1 {
		t.Fatalf("spawn attempts after tick 1=%d; want 1", got)
	}
	if _, ok := h.FindEvent(obs.EventSpawnFailed); !ok {
		t.Fatalf("spawn.failed not emitted on tick 1")
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule tick 2: %v", err)
	}
	if got := fail.calls.Load(); got != 1 {
		t.Fatalf("spawn attempts after tick 2=%d; want 1 (backoff must suppress immediate retry)", got)
	}
	if _, ok := h.FindEvent(obs.EventSpawnBackoffSkipped); !ok {
		t.Fatalf("spawn.backoff_skipped not emitted on suppressed tick 2")
	}
}

// TestScheduleOnce_MissingItemBodyHoldsItem asserts a work_item whose
// brief body is unavailable is NOT spawned blind — it is held back and
// spawn.held_no_body is emitted so the operator can fix the brief
// (MAY-81). Supersedes the prior spawn-blind behavior.
func TestScheduleOnce_MissingItemBodyHoldsItem(t *testing.T) {
	ctx := context.Background()
	o, stub, db, _ := newHarness(t, 1)
	o.cfg.ItemBody = func(context.Context, string) (string, bool) { return "", false }
	h := obstest.New()
	o.log = slog.New(h)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(stub.Calls()) != 0 {
		t.Fatalf("spawner called %d times; want 0 (item must be held when body missing)", len(stub.Calls()))
	}
	if _, ok := h.FindEvent(obs.EventSpawnHeldNoBody); !ok {
		t.Fatalf("spawn.held_no_body not emitted on missing body")
	}
	// Held item must roll back to pending (lock released) so a later
	// tick can re-attempt once the brief lands — not stranded in
	// spawning with a held lock.
	pending, err := db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		t.Fatalf("list pending agents: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending agents=%d; want 1 (item must roll back to pending)", len(pending))
	}
	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("list spawning agents: %v", err)
	}
	if len(spawning) != 0 {
		t.Fatalf("agent stranded in spawning: %d; want 0", len(spawning))
	}
	locks, err := db.ListLocks(ctx)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("locks not released after hold: %d; want 0", len(locks))
	}
}

// TestScheduleOnce_PresentItemBodySpawns asserts the happy path is
// unaffected: a resolvable brief body spawns normally (MAY-81 guard).
func TestScheduleOnce_PresentItemBodySpawns(t *testing.T) {
	ctx := context.Background()
	o, stub, _, _ := newHarness(t, 1)
	o.cfg.ItemBody = func(_ context.Context, id string) (string, bool) { return "BODY-" + id, true }

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	calls := stub.Calls()
	if len(calls) != 1 {
		t.Fatalf("spawner calls=%d; want 1", len(calls))
	}
	if calls[0].ItemBody == "" {
		t.Fatalf("ItemBody empty on present-body spawn")
	}
}

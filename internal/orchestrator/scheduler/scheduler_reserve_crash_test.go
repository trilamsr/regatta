package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// crashingReserveDB wraps *state.DB so the test can panic from inside
// the reservation tx after the row updates have been staged. The
// scheduler's WithTx wrapper must let the panic unwind without
// committing — the deferred Rollback is the safety net.
type crashingReserveDB struct {
	*state.DB
	transitionPanic bool
}

func (c *crashingReserveDB) TransitionAgentTx(ctx context.Context, tx *sql.Tx, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
	if c.transitionPanic {
		// Simulate a process crash partway through the reservation tx
		// after TryAcquireLocksTx has already written lock rows. The
		// deferred Rollback in WithTx must roll those back so the next
		// tick rediscovers the agent without a stranded lock.
		panic("simulated mid-reservation crash")
	}
	return c.DB.TransitionAgentTx(ctx, tx, id, next, mut)
}

// TestTick_CrashMidReservation_LeavesNoOrphans pins the single-tx rollback contract for issue #88.
func TestTick_CrashMidReservation_LeavesNoOrphans(t *testing.T) {
	ctx := context.Background()
	realDB := statetest.OpenDB(t)
	cdb := &crashingReserveDB{DB: realDB, transitionPanic: true}

	seedPlanned(t, realDB, "WORK-1", "server")

	sch := newWithDB(cdb, Config{
		LockTTL:  time.Minute,
		Hotspots: func(string) []string { return []string{"package.json"} },
	})

	func() {
		defer func() { _ = recover() }()
		_, _ = sch.Tick(ctx)
	}()

	locks, err := realDB.ListLocks(ctx)
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("locks=%d want 0 — crash mid-reservation stranded lock rows", len(locks))
	}

	spawning, _ := realDB.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%d want 0 — crash left agent in spawning without locks", len(spawning))
	}

	cdb.transitionPanic = false
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("tick 2 reserved=%d want 1 — orphan state blocked recovery", len(ids))
	}
}

// TestTick_LockHeldRollsBackPendingUpsert pins ErrLockHeld rollback semantics inside the reservation tx.
func TestTick_LockHeldRollsBackPendingUpsert(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)

	holder, err := db.UpsertPending(ctx, "OTHER", "server")
	if err != nil {
		t.Fatalf("upsert holder: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, holder.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("transition holder to spawning: %v", err)
	}
	if err := db.TryAcquireLocks(ctx, []string{"package.json"}, holder.ID, time.Minute); err != nil {
		t.Fatalf("pre-acquire: %v", err)
	}

	seedPlanned(t, db, "WORK-1", "server")
	sch := New(db, Config{
		LockTTL:  time.Minute,
		Hotspots: func(string) []string { return []string{"package.json"} },
	})

	ids, err := sch.Tick(ctx)
	if err != nil && !errors.Is(err, state.ErrLockHeld) {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%d want 0 — lock-held should have skipped reservation", len(ids))
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 1 {
		t.Fatalf("locks=%d want 1 (holder's only) — lock-held path leaked a row", len(locks))
	}
	if locks[0].AgentID != holder.ID {
		t.Fatalf("lock agent=%d want %d", locks[0].AgentID, holder.ID)
	}
}

package state

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// newTestDB returns a private on-disk sqlite database for the test.
// Each test gets its own file under t.TempDir(); shared-cache
// :memory: DSNs leak across tests in the same binary, so we avoid
// them entirely.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	return openTestDB(t, filepath.Join(t.TempDir(), "state.db"))
}

func openTestDB(t *testing.T, path string) *DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenAppliesSchema(t *testing.T) {
	db := newTestDB(t)
	var v int
	if err := db.SQL().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Fatalf("schema_version=%d want %d", v, CurrentSchemaVersion)
	}
}

func TestOpenIsIdempotentAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db := openTestDB(t, path)
	if _, err := db.UpsertPending(context.Background(), "WORK-1", "server"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db2 := openTestDB(t, path)
	var v int
	if err := db2.SQL().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("reopen schema_version: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Fatalf("schema_version=%d want %d", v, CurrentSchemaVersion)
	}
	agents, err := db2.ListAgentsByState(context.Background(), AgentPending)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(agents) != 1 || agents[0].WorkItemID != "WORK-1" {
		t.Fatalf("reopen lost state: %+v", agents)
	}
}

func TestUpsertPendingIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a1, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	a2, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("upsert returned different IDs: %d vs %d", a1.ID, a2.ID)
	}
}

func TestTransitionAgentHappyPath(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	pid := 4242
	sess := "sess-abc"
	if _, err := db.TransitionAgent(ctx, a.ID, AgentSpawning, AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
		t.Fatalf("spawn transition: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != AgentSpawning || got.PID != pid || got.SessionID != sess {
		t.Fatalf("unexpected agent after transition: %+v", got)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, AgentRunning, AgentMutation{}); err != nil {
		t.Fatalf("running transition: %v", err)
	}
}

func TestTransitionAgentRejectsInvalidEdge(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, AgentDone, AgentMutation{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition got %v", err)
	}
}

func TestListAgentsByState(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, id := range []string{"A", "B", "C"} {
		if _, err := db.UpsertPending(ctx, id, "server"); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}
	agents, err := db.ListAgentsByState(ctx, AgentPending)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3", len(agents))
	}
}

func TestLockAcquisitionAndExpiry(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	b, err := db.UpsertPending(ctx, "WORK-2", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	clock := time.Unix(1_700_000_000, 0).UTC()
	db.SetClock(func() time.Time { return clock })

	if err := db.TryAcquireLock(ctx, "package.json", a.ID, 5*time.Minute); err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	if err := db.TryAcquireLock(ctx, "package.json", b.ID, 5*time.Minute); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("want ErrLockHeld, got %v", err)
	}

	// Same agent re-acquires successfully (heartbeat refresh).
	if err := db.TryAcquireLock(ctx, "package.json", a.ID, 5*time.Minute); err != nil {
		t.Fatalf("re-acquire a: %v", err)
	}

	// Advance past TTL: B steals.
	clock = clock.Add(10 * time.Minute)
	if err := db.TryAcquireLock(ctx, "package.json", b.ID, 5*time.Minute); err != nil {
		t.Fatalf("steal: %v", err)
	}

	locks, err := db.ListLocks(ctx)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	if len(locks) != 1 || locks[0].AgentID != b.ID {
		t.Fatalf("unexpected lock state: %+v", locks)
	}
}

func TestExpireStaleLocks(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	clock := time.Unix(1_700_000_000, 0).UTC()
	db.SetClock(func() time.Time { return clock })
	if err := db.TryAcquireLock(ctx, "x", a.ID, time.Minute); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	clock = clock.Add(2 * time.Minute)
	n, err := db.ExpireStaleLocks(ctx, time.Minute)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d locks, want 1", n)
	}
}

func TestRecordEvent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawned", `{"pid":42}`); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := db.RecordEvent(ctx, 0, "system_start", ""); err != nil {
		t.Fatalf("record nil agent: %v", err)
	}
	events, err := db.ListEvents(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].PayloadJSON != `{"pid":42}` {
		t.Fatalf("payload mismatch: %q", events[0].PayloadJSON)
	}
	if events[1].AgentID.Valid {
		t.Fatalf("system event should have null agent_id, got %+v", events[1].AgentID)
	}
}

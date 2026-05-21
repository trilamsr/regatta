package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// failingSpawner returns an error on every Spawn. ScheduleOnce must
// roll the agent back through crashed → pending so a future tick can
// retry, never leaving it stuck in spawning.
type failingSpawner struct{ calls atomic.Int64 }

func (s *failingSpawner) Spawn(context.Context, spawner.Request) (spawner.Result, error) {
	s.calls.Add(1)
	return spawner.Result{}, fmt.Errorf("synthetic spawn failure")
}

func TestScheduleOnceRollsBackOnSpawnerFailure(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)

	bad := &failingSpawner{}
	o.spawner = bad

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if got := bad.calls.Load(); got != 1 {
		t.Fatalf("spawner called %d times, want 1", got)
	}

	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after rollback=%d, want 1", len(pending))
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("agent stuck spawning: %+v", spawning)
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 0 {
		t.Fatalf("locks not released after rollback: %+v", locks)
	}
	events, _ := db.ListEvents(ctx, 50)
	sawFailure := false
	for _, e := range events {
		if e.Kind == "spawn_failed" {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatal("spawn_failed event not recorded")
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule 2: %v", err)
	}
	if got := bad.calls.Load(); got != 2 {
		t.Fatalf("spawner called %d times after retry, want 2", got)
	}
}

func TestRunConcurrentWithRecoverIsRaceFree(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 6; i++ {
		writeAdvItem(t, filepath.Join(dir, ".regatta", "items", string(rune('A'+i))+".md"), string(rune('A'+i)))
	}
	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: dir})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "state.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	o := New(db, ad, spawner.NewStub(), Config{
		PollInterval: 5 * time.Millisecond, TickInterval: 5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond, LockTTL: time.Hour,
	})
	o.SetLogger(t.Logf)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- o.Run(ctx) }()

	recoverErrs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		go func() { recoverErrs <- o.Recover(context.Background()) }()
	}
	for i := 0; i < 50; i++ {
		select {
		case err := <-recoverErrs:
			if err != nil {
				t.Fatalf("concurrent recover error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("recover goroutine never returned")
		}
	}

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run never returned")
	}
}

func TestHeartbeatKeepsActiveLockAlive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeAdvItem(t, filepath.Join(dir, ".regatta", "items", "A.md"), "A")
	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: dir})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "state.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	db, err := state.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clock := time.Unix(1_700_000_000, 0).UTC()
	db.SetClock(func() time.Time { return clock })

	o := New(db, ad, spawner.NewStub(), Config{
		PollInterval: time.Minute, TickInterval: time.Minute,
		HeartbeatInterval: time.Minute,
		LockTTL:           time.Minute,
		Hotspots:          func(string) []string { return []string{"hotspot"} },
	})
	o.SetLogger(t.Logf)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 1 {
		t.Fatalf("expected 1 lock after schedule, got %d", len(locks))
	}

	for i := 0; i < 5; i++ {
		clock = clock.Add(30 * time.Second)
		if err := o.Heartbeat(ctx); err != nil {
			t.Fatalf("heartbeat: %v", err)
		}
	}
	locks, _ = db.ListLocks(ctx)
	if len(locks) != 1 {
		t.Fatalf("active lock evicted despite heartbeats: %+v", locks)
	}

	clock = clock.Add(2 * time.Minute)
	if _, err := db.ExpireStaleLocks(ctx, time.Minute); err != nil {
		t.Fatalf("expire: %v", err)
	}
	locks, _ = db.ListLocks(ctx)
	if len(locks) != 0 {
		t.Fatalf("stale lock not evicted: %+v", locks)
	}
}

func writeAdvItem(t *testing.T, path, name string) {
	t.Helper()
	body := fmt.Sprintf(`---
id: ITEM-%s
title: adv %s
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: only criterion
`, name, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

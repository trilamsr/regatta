package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/schemas"
)

const tmplItem = `---
id: ITEM-%s
title: Item %s
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: only criterion
`

func newHarness(t *testing.T, count int) (*Orchestrator, *spawner.Stub, *state.DB, string) {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		name := string(rune('A' + i))
		full := filepath.Join(dir, ".regatta", "items", name+".md")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := []byte(replace(tmplItem, name))
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
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
	stub := spawner.NewStub()
	o := New(db, ad, stub, Config{
		PollInterval: time.Second, TickInterval: time.Second,
		HeartbeatInterval: time.Second, LockTTL: time.Minute,
	})
	o.SetLogger(t.Logf)
	return o, stub, db, dir
}

func replace(s, name string) string {
	return strings.ReplaceAll(s, "%s", name)
}

func TestPollAndScheduleEndToEnd(t *testing.T) {
	ctx := context.Background()
	o, stub, db, _ := newHarness(t, 2)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 2 {
		t.Fatalf("pending=%d, want 2", len(pending))
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	running, _ := db.ListAgentsByState(ctx, state.AgentRunning)
	if len(running) != 2 {
		t.Fatalf("running=%d, want 2", len(running))
	}
	calls := stub.Calls()
	if len(calls) != 2 {
		t.Fatalf("spawner calls=%d, want 2", len(calls))
	}
	for _, a := range running {
		if a.PID >= 0 || a.SessionID == "" {
			t.Fatalf("agent %d missing spawn identity: %+v", a.ID, a)
		}
	}
}

func TestPollIsIdempotent(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending=%d, want 1", len(pending))
	}
}

func TestRecoverRequeuesDeadAgents(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	// Stub spawner uses negative PIDs which pidAlive() treats as dead,
	// so Recover should requeue every running agent.
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after recover=%d, want 1", len(pending))
	}
	running, _ := db.ListAgentsByState(ctx, state.AgentRunning)
	if len(running) != 0 {
		t.Fatalf("running after recover=%d, want 0", len(running))
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 0 {
		t.Fatalf("locks not released: %+v", locks)
	}
}

// failingAdapter returns an error on every List call. Used to prove
// Run survives a broken SpecAdapter without crashing.
type failingAdapter struct{ calls int }

func (a *failingAdapter) List(ctx context.Context) ([]schemas.WorkItem, error) {
	a.calls++
	return nil, fmt.Errorf("synthetic adapter failure %d", a.calls)
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

func TestRunSurvivesFailingAdapter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ad := &failingAdapter{}
	o := New(db, ad, spawner.NewStub(), Config{
		PollInterval: 10 * time.Millisecond, TickInterval: 10 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond, LockTTL: time.Minute,
	})
	o.SetLogger(t.Logf)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v for failing adapter; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	if ad.calls < 2 {
		t.Fatalf("expected adapter polled ≥2 times despite failures, got %d", ad.calls)
	}
}

func TestRecoverIsIdempotent(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover 1: %v", err)
	}
	// Second call: every non-terminal agent is now pending, so
	// Recover should be a no-op (no panics, no extra requeues).
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover 2: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after 2x recover=%d, want 1", len(pending))
	}
}

// TestPollPropagatesLaneChange pins the orchestrator end-to-end on the
// lane-drift bug in state.UpsertPending: a markdown item whose `lane:`
// frontmatter is rewritten must be reflected on the existing agent row
// by the next PollOnce, not stuck in its original lane (which would
// defeat per-lane caps).
func TestPollPropagatesLaneChange(t *testing.T) {
	ctx := context.Background()
	o, _, db, dir := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	agents, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(agents) != 1 || agents[0].Lane != "server" {
		t.Fatalf("initial state mismatch: %+v", agents)
	}

	file := filepath.Join(dir, ".regatta", "items", "A.md")
	updated := []byte(`---
id: ITEM-A
title: Item A
lane: client
status: planned
---

## Acceptance criteria

- [planned] c1: only criterion
`)
	if err := os.WriteFile(file, updated, 0o644); err != nil {
		t.Fatalf("rewrite item file: %v", err)
	}

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	agents, _ = db.ListAgentsByState(ctx, state.AgentPending)
	if len(agents) != 1 || agents[0].Lane != "client" {
		t.Fatalf("expected lane=client after re-poll; got %+v", agents)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	o, _, _, _ := newHarness(t, 1)
	o.cfg.PollInterval = 10 * time.Millisecond
	o.cfg.TickInterval = 10 * time.Millisecond
	o.cfg.HeartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

package spawner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func openSpawnerTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedPlannedWI(t *testing.T, db *state.DB, id string) {
	t.Helper()
	w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id, Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestStubReturnsUniqueIdentity(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	a, err := s.Spawn(ctx, Request{AgentID: 1, WorkItemID: "WORK-1", Lane: "server"})
	if err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	b, err := s.Spawn(ctx, Request{AgentID: 2, WorkItemID: "WORK-2", Lane: "server"})
	if err != nil {
		t.Fatalf("spawn b: %v", err)
	}
	if a.PID == b.PID || a.SessionID == b.SessionID {
		t.Fatalf("stub returned colliding identity: %+v vs %+v", a, b)
	}
	if a.PID >= 0 || b.PID >= 0 {
		t.Fatalf("stub PIDs must be negative; got %d, %d", a.PID, b.PID)
	}
}

func TestStubRecordsCalls(t *testing.T) {
	s := NewStub()
	_, _ = s.Spawn(context.Background(), Request{AgentID: 7, WorkItemID: "WORK-7"})
	calls := s.Calls()
	if len(calls) != 1 || calls[0].AgentID != 7 {
		t.Fatalf("unexpected calls log: %+v", calls)
	}
}

func TestStubConcurrentSafe(t *testing.T) {
	s := NewStub()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = s.Spawn(context.Background(), Request{AgentID: int64(i)})
		}(i)
	}
	wg.Wait()
	if got := len(s.Calls()); got != 32 {
		t.Fatalf("got %d calls, want 32", got)
	}
}

// TestSpawner_WritesJournalBeforeMerge pins the spec §3.9 atomicity
// contract: Complete must write the outputs journal entry and only
// then mark the work_item merged. Scheduler tick step-0 reads
// ListPendingEdgesFromMerged and then GetLatestOutput — the journal
// row must exist when the work_item is first observed at status=merged.
func TestSpawner_WritesJournalBeforeMerge(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	sp := NewStubWithDB(db)
	if _, err := sp.Spawn(ctx, Request{AgentID: 1, WorkItemID: "F-1", Lane: "server"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := sp.Complete(ctx, "F-1", json.RawMessage(`{"completed":true}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	entry, err := db.GetLatestOutput(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetLatestOutput: %v", err)
	}
	if entry.OutputJSON != `{"completed":true}` {
		t.Fatalf("journal payload mismatch: %q", entry.OutputJSON)
	}

	wi, err := db.GetWorkItem(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status != state.WorkStatusMerged {
		t.Fatalf("status=%q, want merged", wi.Status)
	}
}

// TestSpawnerComplete_AtomicJournalAndMerge mutation-verifies the
// order: if AppendOutput fails the transition must NOT happen.
// Without journal-first ordering a scheduler tick could see
// status=merged on a work_item whose outputs row never materialised,
// stalling edge eval (ErrJournalNotFound spin per spec §3.9 trap-list).
func TestSpawnerComplete_AtomicJournalAndMerge(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	sp := NewStubWithDB(db)

	// Invalid (non-JSON) payload makes AppendOutput's canonicaliser fail.
	err := sp.Complete(ctx, "F-1", json.RawMessage(`not-json`))
	if err == nil {
		t.Fatal("Complete with invalid payload must fail")
	}

	// Journal must be empty.
	if _, err := db.GetLatestOutput(ctx, "F-1"); !errors.Is(err, state.ErrJournalNotFound) {
		t.Fatalf("expected ErrJournalNotFound, got %v", err)
	}
	// Work item must NOT have transitioned to merged.
	wi, err := db.GetWorkItem(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status == state.WorkStatusMerged {
		t.Fatal("transition happened despite AppendOutput failure")
	}
}

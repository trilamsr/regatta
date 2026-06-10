package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newWorkItemsTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), DSN(filepath.Join(t.TempDir(), "wi.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// fixedClockDB opens a fresh DB whose constructor-bound clock returns
// t0 forever. Tests that don't need to advance time pick this.
func fixedClockDB(t *testing.T, t0 time.Time) *DB {
	t.Helper()
	db, err := OpenWithClock(context.Background(), DSN(filepath.Join(t.TempDir(), "wi.db")), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUpsertWorkItem_RoundTrip(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	item := WorkItem{
		ID: "PROG-1", Kind: KindProgram, Title: "test prog",
		Lane: "server", Status: WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(ctx, item, SourceAdapter, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	got, err := db.GetWorkItem(ctx, "PROG-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if got.Title != "test prog" || got.Source != SourceAdapter || got.Kind != KindProgram {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestUpsertWorkItem_UpdatePreservesCreatedAt(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Minute)
	db := newWorkItemsTestDB(t)
	ctx := context.Background()

	item := WorkItem{ID: "F-1", Kind: KindFeature, Title: "v1", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}
	item.Title = "v2"
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t1); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetWorkItem(ctx, "F-1")
	if !got.CreatedAt.Equal(t0) {
		t.Fatalf("CreatedAt=%v want %v (must persist across updates)", got.CreatedAt, t0)
	}
	if !got.UpdatedAt.Equal(t1) {
		t.Fatalf("UpdatedAt=%v want %v", got.UpdatedAt, t1)
	}
	if got.Title != "v2" {
		t.Fatalf("Title=%q want v2 (update not applied)", got.Title)
	}
}

func TestGetWorkItem_NotFound(t *testing.T) {
	db := newWorkItemsTestDB(t)
	_, err := db.GetWorkItem(context.Background(), "missing")
	if !errors.Is(err, ErrWorkItemNotFound) {
		t.Fatalf("err=%v want ErrWorkItemNotFound", err)
	}
}

func TestTombstoneBySource_SourceScoped(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Minute)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	adapter := WorkItem{ID: "ADAPT-1", Kind: KindFeature, Title: "a", Lane: "server", Status: WorkStatusPlanned}
	brief := WorkItem{ID: "BRIEF-1", Kind: KindFeature, Title: "b", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, adapter, SourceAdapter, t0); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, brief, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}

	archived, err := db.TombstoneBySource(ctx, SourceBrief, t1)
	if err != nil {
		t.Fatalf("TombstoneBySource: %v", err)
	}
	if len(archived) != 1 || archived[0] != "BRIEF-1" {
		t.Fatalf("archived=%v want [BRIEF-1]", archived)
	}

	got, _ := db.GetWorkItem(ctx, "ADAPT-1")
	if got.Status != WorkStatusPlanned {
		t.Fatalf("ADAPT-1.status=%s want planned (per-source must not stomp adapter rows)", got.Status)
	}
	got, _ = db.GetWorkItem(ctx, "BRIEF-1")
	if got.Status != WorkStatusArchived {
		t.Fatalf("BRIEF-1.status=%s want archived", got.Status)
	}
}

func TestTombstoneBySource_SkipsAlreadyArchived(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	item := WorkItem{ID: "F-1", Kind: KindFeature, Title: "x", Lane: "server", Status: WorkStatusArchived}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}

	archived, err := db.TombstoneBySource(context.Background(), SourceBrief, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 0 {
		t.Fatalf("archived=%v want empty (already-archived rows must not be re-tombstoned)", archived)
	}
}

// TestTombstoneBySource_WithdrawsPendingAndCrashedAgents asserts the cascade-withdraw of bound pending+crashed agents in the tombstone tx (#1208 retro).
func TestTombstoneBySource_WithdrawsPendingAndCrashedAgents(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	// Three work_items from the same source; one pending agent each.
	// Then mutate two of the agents to crashed (simulates post-recover
	// state that has not yet been re-queued to pending).
	ids := []string{"BRIEF-A", "BRIEF-B", "BRIEF-C"}
	for _, id := range ids {
		wi := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: WorkStatusPlanned}
		if err := db.UpsertWorkItem(ctx, wi, SourceBrief, t0); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
		if _, err := db.UpsertPending(ctx, id, "server"); err != nil {
			t.Fatalf("upsert pending agent for %s: %v", id, err)
		}
	}
	// Drop two agents into crashed via direct UPDATE — emulates
	// orchestrator.Recover's spawning→crashed step prior to the
	// crashed→pending requeue.
	if _, err := db.sql.ExecContext(ctx,
		`UPDATE agents SET state=? WHERE work_item_id IN (?, ?)`,
		string(AgentCrashed), "BRIEF-B", "BRIEF-C"); err != nil {
		t.Fatalf("force crashed: %v", err)
	}

	archived, err := db.TombstoneBySource(ctx, SourceBrief, t1)
	if err != nil {
		t.Fatalf("TombstoneBySource: %v", err)
	}
	if len(archived) != 3 {
		t.Fatalf("archived=%v want 3", archived)
	}

	// Every bound agent (pending OR crashed) must now be withdrawn —
	// otherwise the next Recover→Tick will re-dispatch them.
	for _, id := range ids {
		a, err := db.GetAgentByWorkItemID(ctx, id)
		if err != nil {
			t.Fatalf("get agent %s: %v", id, err)
		}
		if a.State != AgentWithdrawn {
			t.Fatalf("agent for %s state=%s want withdrawn (cascade missing → ghost recovery storm on restart)", id, a.State)
		}
	}
}

// TestTombstoneBySource_LeavesActiveAgentsUntouched pins the cascade-soft invariant: in-flight agents (running/spawning/etc.) survive a tombstone (#1208 retro).
func TestTombstoneBySource_LeavesActiveAgentsUntouched(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	wi := WorkItem{ID: "BRIEF-X", Kind: KindFeature, Title: "x", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, wi, SourceBrief, t0); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := db.UpsertPending(ctx, "BRIEF-X", "server"); err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	// Walk the agent forward to running so the cascade-soft invariant
	// has a concrete in-flight target.
	if _, err := db.sql.ExecContext(ctx,
		`UPDATE agents SET state=? WHERE work_item_id=?`,
		string(AgentRunning), "BRIEF-X"); err != nil {
		t.Fatalf("force running: %v", err)
	}

	if _, err := db.TombstoneBySource(ctx, SourceBrief, t1); err != nil {
		t.Fatalf("TombstoneBySource: %v", err)
	}

	a, err := db.GetAgentByWorkItemID(ctx, "BRIEF-X")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if a.State != AgentRunning {
		t.Fatalf("running agent state=%s want running (cascade-soft: in-flight agents keep running)", a.State)
	}
}

func TestCascadeArchiveChildren_FlipsStatusOnly(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	parent := WorkItem{ID: "PROG-1", Kind: KindProgram, Title: "p", Lane: "server", Status: WorkStatusPlanned}
	child := WorkItem{ID: "F-1", Kind: KindFeature, Title: "c", Lane: "server", Status: WorkStatusRunning, ParentProgramID: "PROG-1"}
	if err := db.UpsertWorkItem(ctx, parent, SourceAdapter, t0); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, child, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}

	if _, err := db.CascadeArchiveChildren(ctx, "PROG-1", t0); err != nil {
		t.Fatalf("CascadeArchiveChildren: %v", err)
	}

	got, _ := db.GetWorkItem(ctx, "F-1")
	if got.Status != WorkStatusArchived {
		t.Fatalf("F-1.status=%s want archived", got.Status)
	}
	// Cascade-SOFT: agents table untouched. Verify no agent row was
	// created or modified for F-1.
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE work_item_id=?`, "F-1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("agents table for F-1 has %d rows; cascade-soft must not touch agents", count)
	}
}

func TestUpsertWorkItem_RoundTripsDependsOnFeatures(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	item := WorkItem{
		ID: "F-2", Kind: KindFeature, Title: "depends", Lane: "server",
		Status: WorkStatusPlanned, DependsOnFeatures: []string{"F-1", "F-A"},
	}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetWorkItem(ctx, "F-2")
	if len(got.DependsOnFeatures) != 2 || got.DependsOnFeatures[0] != "F-1" || got.DependsOnFeatures[1] != "F-A" {
		t.Fatalf("DependsOnFeatures=%v want [F-1 F-A]", got.DependsOnFeatures)
	}
}

func TestUpsertWorkItem_RejectsMalformedAcceptanceJSON(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	item := WorkItem{
		ID: "F-bad", Kind: KindFeature, Title: "garbage",
		Lane: "server", Status: WorkStatusPlanned,
		AcceptanceJSON: "{not valid json",
	}
	err := db.UpsertWorkItem(ctx, item, SourceBrief, t0)
	if err == nil {
		t.Fatal("UpsertWorkItem accepted malformed AcceptanceJSON; must reject")
	}
}

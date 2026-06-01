package spawner

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestReconcileOrphans_ReplaysMergeAfterCrashedComplete pins the core contract.
func TestReconcileOrphans_ReplaysMergeAfterCrashedComplete(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	// Simulate crash: write the journal entry but do NOT flip the
	// work_item to merged. Byte-identical to a SIGKILL between the
	// two writes inside Complete (spec §3.9 trap-list).
	if _, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"completed":true}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}
	wi, err := db.GetWorkItem(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status == state.WorkStatusMerged {
		t.Fatalf("precondition violated: status already merged before reconcile")
	}

	fixed := time.Unix(1_700_000_000, 0).UTC()
	sp := New(Config{DB: db, Clock: func() time.Time { return fixed }})

	n, err := sp.ReconcileOrphans(ctx)
	if err != nil {
		t.Fatalf("ReconcileOrphans: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled=%d, want 1", n)
	}

	wi, err = db.GetWorkItem(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetWorkItem post-reconcile: %v", err)
	}
	if wi.Status != state.WorkStatusMerged {
		t.Fatalf("status=%q, want merged after Reconcile", wi.Status)
	}
}

// TestReconcileOrphans_NoopWhenConsistent guards the idempotent path.
func TestReconcileOrphans_NoopWhenConsistent(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	sp := New(Config{DB: db})
	if _, err := sp.Spawn(ctx, Request{AgentID: 1, WorkItemID: "F-1", Lane: "server"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := sp.Complete(ctx, "F-1", json.RawMessage(`{"completed":true}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	n, err := sp.ReconcileOrphans(ctx)
	if err != nil {
		t.Fatalf("ReconcileOrphans: %v", err)
	}
	if n != 0 {
		t.Fatalf("reconciled=%d, want 0 (already consistent)", n)
	}
}

// TestReconcileOrphans_IgnoresWorkItemsWithoutJournal asserts the reconciler is one-sided.
func TestReconcileOrphans_IgnoresWorkItemsWithoutJournal(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")
	seedPlannedWI(t, db, "F-2")

	if _, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"v":1}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	sp := New(Config{DB: db})
	n, err := sp.ReconcileOrphans(ctx)
	if err != nil {
		t.Fatalf("ReconcileOrphans: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled=%d, want 1 (only F-1 had a journal)", n)
	}

	wi2, err := db.GetWorkItem(ctx, "F-2")
	if err != nil {
		t.Fatalf("GetWorkItem F-2: %v", err)
	}
	if wi2.Status != state.WorkStatusPlanned {
		t.Fatalf("F-2 status=%q, want planned (untouched)", wi2.Status)
	}
}

// TestReconcileOrphans_Property fuzzes random crash points across N items.
func TestReconcileOrphans_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ctx := context.Background()
		db, err := state.Open(ctx, state.DSN(filepath.Join(t.TempDir(), "p.db")))
		if err != nil {
			rt.Fatalf("Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		n := rapid.IntRange(1, 8).Draw(rt, "n")
		journaled := map[string]bool{}
		for i := 0; i < n; i++ {
			id := stringID(i)
			w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id, Lane: "server", Status: state.WorkStatusPlanned}
			if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, time.Now()); err != nil {
				rt.Fatalf("seed %s: %v", id, err)
			}
			// Three random crash points per item:
			//   0 = no journal (normal pending)
			//   1 = journal only (crashed mid-Complete — orphan)
			//   2 = journal + merge (clean completion)
			outcome := rapid.IntRange(0, 2).Draw(rt, "outcome_"+id)
			if outcome >= 1 {
				if _, err := db.AppendOutput(ctx, id, json.RawMessage(`{"v":1}`)); err != nil {
					rt.Fatalf("AppendOutput %s: %v", id, err)
				}
				journaled[id] = true
			}
			if outcome == 2 {
				wi, err := db.GetWorkItem(ctx, id)
				if err != nil {
					rt.Fatalf("GetWorkItem %s: %v", id, err)
				}
				wi.Status = state.WorkStatusMerged
				if err := db.UpsertWorkItem(ctx, wi, wi.Source, time.Now()); err != nil {
					rt.Fatalf("merge %s: %v", id, err)
				}
			}
		}

		sp := New(Config{DB: db})
		if _, err := sp.ReconcileOrphans(ctx); err != nil {
			rt.Fatalf("ReconcileOrphans: %v", err)
		}
		// Re-run to assert idempotency.
		if _, err := sp.ReconcileOrphans(ctx); err != nil {
			rt.Fatalf("ReconcileOrphans 2nd: %v", err)
		}

		for i := 0; i < n; i++ {
			id := stringID(i)
			wi, err := db.GetWorkItem(ctx, id)
			if err != nil {
				rt.Fatalf("GetWorkItem %s: %v", id, err)
			}
			if journaled[id] && wi.Status != state.WorkStatusMerged {
				rt.Fatalf("%s journal present but status=%q, want merged", id, wi.Status)
			}
			if !journaled[id] && wi.Status != state.WorkStatusPlanned {
				rt.Fatalf("%s no journal but status=%q, want planned (reconciler must not touch)", id, wi.Status)
			}
		}
	})
}

func stringID(i int) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return "F-" + string(alphabet[i%len(alphabet)])
}

// TestReconcileOrphans_EmitsSpawnReconciled pins the obs contract.
func TestReconcileOrphans_EmitsSpawnReconciled(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	if _, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"v":1}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	h := obstest.New()
	sp := New(Config{DB: db, Logger: slog.New(h)})
	if _, err := sp.ReconcileOrphans(ctx); err != nil {
		t.Fatalf("ReconcileOrphans: %v", err)
	}

	rec, ok := h.FindEvent(obs.EventSpawnReconciled)
	if !ok {
		t.Fatalf("missing %q event; got %d records", obs.EventSpawnReconciled, len(h.Records()))
	}
	if v, ok := recordAttr(rec, string(obs.KeyWorkItemID)); !ok || v.String() != "F-1" {
		t.Errorf("%s missing work_item_id=F-1; got %v ok=%v", obs.EventSpawnReconciled, v, ok)
	}
}

package scheduler

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestTick_SkipsOrphanWhenWorkItemArchived pins the scheduler safety-net: orphans bound to archived work_items must not dispatch (#1208 retro).
func TestTick_SkipsOrphanWhenWorkItemArchived(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)

	// Seed a planned work_item, materialize a pending agent (the
	// orphan-recovery shape), then tombstone the work_item to
	// archived directly via SQL — emulating the post-restart state
	// where the cascade did not fire for any reason.
	seedPlanned(t, db, "ARCHIVED-1", "server")
	if _, err := db.UpsertPending(ctx, "ARCHIVED-1", "server"); err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE work_items SET status=? WHERE id=?`,
		string(state.WorkStatusArchived), "ARCHIVED-1"); err != nil {
		t.Fatalf("force archived: %v", err)
	}

	h := obstest.New()
	sch := New(db, Config{LockTTL: time.Minute, Logger: slog.New(h)})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%d want 0 — orphan bound to archived work_item must be skipped", len(ids))
	}

	// Pending agent must remain in pending (gate skipped, did not
	// advance). The cascade fix is the authoritative state-flip;
	// the safety net only prevents dispatch.
	a, err := db.GetAgentByWorkItemID(ctx, "ARCHIVED-1")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if a.State != state.AgentPending {
		t.Fatalf("agent state=%s want pending — guard advanced an agent it should have skipped", a.State)
	}

	// Skip reason must be observable in operator logs so the next
	// orphan storm is diagnosable.
	var sawSkip bool
	for _, r := range h.Records() {
		if r.Message != "scheduler.orphan_skipped_archived" {
			continue
		}
		attrs := map[string]string{}
		r.Attrs(func(at slog.Attr) bool {
			attrs[at.Key] = at.Value.String()
			return true
		})
		if attrs[string(obs.KeyWorkItemID)] == "ARCHIVED-1" {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Fatal("missing scheduler.orphan_skipped_archived log — silent skip breaks operator diagnostics")
	}
}

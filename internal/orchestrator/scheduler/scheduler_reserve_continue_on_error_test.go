package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// failingUpsertDB wraps *state.DB and fails UpsertPendingTx for the
// configured work_item_id. The tx rolls back on error, so no agent row
// is materialized for that id — matching a production row-shape error
// that would crash the upsert (e.g. lane FK violation).
type failingUpsertDB struct {
	*state.DB
	failID string
	err    error
}

func (f *failingUpsertDB) UpsertPendingTx(ctx context.Context, tx *sql.Tx, workItemID, lane string) (*state.Agent, error) {
	if workItemID == f.failID {
		return nil, f.err
	}
	return f.DB.UpsertPendingTx(ctx, tx, workItemID, lane)
}

// TestTick_ContinuesPastPerItemReserveFailure pins issue #95: a per-item upsert error logs and skips, batch continues.
func TestTick_ContinuesPastPerItemReserveFailure(t *testing.T) {
	ctx := context.Background()
	realDB := statetest.OpenDB(t)
	wantErr := errors.New("simulated upsert failure")
	fdb := &failingUpsertDB{DB: realDB, failID: "W-2", err: wantErr}

	for _, id := range []string{"W-1", "W-2", "W-3"} {
		seedPlanned(t, realDB, id, "server")
	}

	h := obstest.New()
	sch := newWithDB(fdb, Config{LockTTL: time.Minute, Logger: slog.New(h)})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v (per-item failure must not abort the batch)", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved=%d want 2 — W-1 and W-3 should have reserved despite W-2 failing", len(ids))
	}

	spawning, err := realDB.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("list spawning: %v", err)
	}
	got := map[string]bool{}
	for _, a := range spawning {
		got[a.WorkItemID] = true
	}
	if !got["W-1"] || !got["W-3"] {
		t.Fatalf("spawning set=%v want W-1 and W-3", got)
	}
	if got["W-2"] {
		t.Fatalf("W-2 unexpectedly reserved despite injected upsert failure")
	}

	var sawPerItem, sawBatchSummary bool
	for _, r := range h.Records() {
		if r.Message != string(obs.EventSchedulerMaterializeFailure) {
			continue
		}
		attrs := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		if attrs[string(obs.KeyWorkItemID)] == "W-2" &&
			attrs[string(obs.KeyReason)] == "reserve_failed" &&
			strings.Contains(attrs[string(obs.KeyErr)], "simulated upsert failure") {
			sawPerItem = true
		}
		if attrs[string(obs.KeyReason)] == "pass_completed_with_failures" {
			sawBatchSummary = true
		}
	}
	if !sawPerItem {
		t.Fatalf("missing per-item materialize_failure log for W-2 — operator cannot diagnose silent skip")
	}
	if !sawBatchSummary {
		t.Fatalf("missing pass_completed_with_failures summary log — batch-level failure count not surfaced")
	}
}

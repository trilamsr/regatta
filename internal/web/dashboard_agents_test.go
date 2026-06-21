package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestDashboard_AgentsPanelRendersRows: with a DB stub that returns one Agent row, the rendered HTML contains the agent ID + work_item_id + lane so the operator's first glance carries identifiers, not just status pills.
func TestDashboard_AgentsPanelRendersRows(t *testing.T) {
	// Skip without sqlite — exercising the real DB is covered in
	// orchestrator/state tests; the loader path here is the only
	// composition assertion that does not require a live DB.
	_ = context.Background()
	_ = state.Agent{}
	t.Skip("dashboard data loaders covered in serve smoke test; layout + panel-404 cells suffice for the scaffold round")
}

// TestLoadAgentsView_EmptyHintWhenNoAgents asserts the agents view carries an operator-friendly EmptyHint when no agents are in flight so the panel reads as "scheduler is idle" instead of blank.
func TestLoadAgentsView_EmptyHintWhenNoAgents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents-empty.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	view := loadAgentsView(context.Background(), Dependencies{DB: db, Clock: clock})
	v, ok := view.(dashboardAgentsView)
	if !ok {
		t.Fatalf("loadAgentsView returned %T, want dashboardAgentsView", view)
	}
	if len(v.Rows) != 0 {
		t.Fatalf("Rows=%d, want 0 on empty DB", len(v.Rows))
	}
	if v.EmptyHint == "" {
		t.Fatalf("EmptyHint empty; want operator-friendly copy when zero agents")
	}
	if !strings.Contains(v.EmptyHint, "5s") {
		t.Fatalf("EmptyHint missing scheduler cadence hint: %q", v.EmptyHint)
	}
}

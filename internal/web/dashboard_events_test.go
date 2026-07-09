package web

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestEventVerb_ExitedWithProviderCreditExhausted asserts agent.exited surfaces exit_reason as colored badge inline (#dashboard-exit-reason-badges).
func TestEventVerb_ExitedWithProviderCreditExhausted(t *testing.T) {
	ev := state.StateEvent{
		Kind:        "agent.exited",
		PayloadJSON: `{"exit_reason":"provider_credit_exhausted","exit_code":1}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 7
	got := string(eventVerb(ev))
	if !strings.Contains(got, "exited") {
		t.Fatalf("missing exited verb: %q", got)
	}
	if !strings.Contains(got, "badge-red") {
		t.Fatalf("missing badge-red class: %q", got)
	}
	if !strings.Contains(got, "provider_credit_exhausted") {
		t.Fatalf("missing exit_reason text: %q", got)
	}
}

// TestEventVerb_SpawnCompletedShowsAgentAndWorkItem pins #1119: spawn.completed surfaces agent_id (Event.AgentID) + work_item_id (PayloadJSON) inline so 12 identical "ready" verbs become scannable.
func TestEventVerb_SpawnCompletedShowsAgentAndWorkItem(t *testing.T) {
	ev := state.StateEvent{
		Kind:        "spawn.completed",
		PayloadJSON: `{"work_item_id":"BUG-1058","pid":12345}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 42
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#42") {
		t.Fatalf("missing agent_id chip #42: %q", got)
	}
	if !strings.Contains(got, "BUG-1058") {
		t.Fatalf("missing work_item_id chip BUG-1058: %q", got)
	}
	if !strings.Contains(got, "ready") {
		t.Fatalf("missing verb 'ready': %q", got)
	}
}

// TestEventVerb_SpawnFailedShowsAgentAndExitReason pins #1119: spawn.failed surfaces agent_id chip + exit_reason badge inline (currently exit_reason fires only for agent.exited).
func TestEventVerb_SpawnFailedShowsAgentAndExitReason(t *testing.T) {
	ev := state.StateEvent{
		Kind:        "spawn.failed",
		PayloadJSON: `{"work_item_id":"BUG-1058","exit_reason":"provider_credit_exhausted"}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 7
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#7") {
		t.Fatalf("missing agent_id chip #7: %q", got)
	}
	if !strings.Contains(got, "BUG-1058") {
		t.Fatalf("missing work_item_id chip BUG-1058: %q", got)
	}
	if !strings.Contains(got, "badge-red") {
		t.Fatalf("missing badge-red for provider_credit_exhausted: %q", got)
	}
	if !strings.Contains(got, "provider_credit_exhausted") {
		t.Fatalf("missing exit_reason text: %q", got)
	}
}

// TestEventVerb_BriefLoadedShowsAgent pins #1119: brief.loaded surfaces agent_id + work_item_id chips so consecutive brief loads are distinguishable inline.
func TestEventVerb_BriefLoadedShowsAgent(t *testing.T) {
	ev := state.StateEvent{
		Kind:        "brief.loaded",
		PayloadJSON: `{"work_item_id":"WORK-99"}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 13
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#13") {
		t.Fatalf("missing agent_id chip #13: %q", got)
	}
	if !strings.Contains(got, "WORK-99") {
		t.Fatalf("missing work_item_id chip WORK-99: %q", got)
	}
	if !strings.Contains(got, "brief") {
		t.Fatalf("missing verb 'brief': %q", got)
	}
}

// TestEventVerb_RecoveredCrashedShowsAgent pins #1119: recovered_crashed surfaces agent_id chip inline so the operator can trace which agent recovered without opening the drawer.
func TestEventVerb_RecoveredCrashedShowsAgent(t *testing.T) {
	ev := state.StateEvent{
		Kind:        "recovered_crashed",
		PayloadJSON: `{}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 99
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#99") {
		t.Fatalf("missing agent_id chip #99: %q", got)
	}
	if !strings.Contains(got, "recovered") {
		t.Fatalf("missing verb 'recovered': %q", got)
	}
}

// TestEventVerb_EmptyAgentIDOmitsChip pins graceful degradation when AgentID.Valid is false — the chip is omitted, not rendered as "agent#0".
func TestEventVerb_EmptyAgentIDOmitsChip(t *testing.T) {
	e := state.StateEvent{Kind: "spawn.completed", PayloadJSON: `{"work_item_id":"BUG-1058"}`}
	got := string(eventVerb(e))
	if strings.Contains(got, "agent #0") || strings.Contains(got, "agent #") {
		t.Fatalf("invalid AgentID rendered as agent#0: %q", got)
	}
	if !strings.Contains(got, "BUG-1058") {
		t.Fatalf("work_item_id chip dropped when AgentID is invalid: %q", got)
	}
}

// TestEventVerb_MalformedPayloadOmitsWorkItemChip pins graceful degradation when PayloadJSON is non-JSON — the chip is omitted, no panic.
func TestEventVerb_MalformedPayloadOmitsWorkItemChip(t *testing.T) {
	e := state.StateEvent{Kind: "spawn.completed", AgentID: sql.NullInt64{Int64: 42, Valid: true}, PayloadJSON: `{not json`}
	got := string(eventVerb(e))
	if strings.Contains(got, "BUG-") {
		t.Fatalf("malformed payload rendered work_item_id: %q", got)
	}
	if !strings.Contains(got, "#42") {
		t.Fatalf("agent #42 dropped when payload malformed: %q", got)
	}
}

// TestLoadEventsView_EmptyHintWhenNoEvents asserts the events view carries an EmptyHint when no events landed in the tail window so the operator does not stare at a blank panel.
func TestLoadEventsView_EmptyHintWhenNoEvents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events-empty.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	view := loadEventsView(context.Background(), Dependencies{DB: db, Clock: clock})
	v, ok := view.(dashboardEventsView)
	if !ok {
		t.Fatalf("loadEventsView returned %T, want dashboardEventsView", view)
	}
	if len(v.Rows) != 0 {
		t.Fatalf("Rows=%d, want 0 on empty DB", len(v.Rows))
	}
	if v.EmptyHint == "" {
		t.Fatalf("EmptyHint empty; want operator-friendly copy when zero events")
	}
}

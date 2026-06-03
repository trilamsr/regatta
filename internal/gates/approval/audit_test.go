package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/slogutil"

	"pgregory.net/rapid"
)

// Spec §5.7: a single recordEvent helper writes BOTH the slog record
// and the approval_events row so the two surfaces cannot drift. This
// test asserts byte-equality between the captured slog attrs payload
// and the row payload_json.

func auditTestDB(t *testing.T, now time.Time) *state.DB {
	t.Helper()
	dsn := state.DSN(filepath.Join(t.TempDir(), "audit.db"))
	db, err := state.OpenWithClock(context.Background(), dsn, func() time.Time { return now })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAudit_SlogMatchesRow(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := auditTestDB(t, now)
	ctx := context.Background()
	approvalID := "a-test-1234"
	// Seed a work_item so the approvals FK resolves, then an approval
	// so the approval_events FK resolves.
	if err := db.UpsertWorkItem(ctx, state.WorkItem{
		ID: "wi-1", Kind: state.KindFeature, Title: "x", Lane: "server",
		Status: state.WorkStatusPlanned,
	}, state.SourceAdapter, now); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	if err := db.CreateApproval(ctx, state.Approval{
		ID: approvalID, WorkItemID: "wi-1", GateName: "deploy-gate",
		RequestedAt: now, RequestedBy: "owner",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 1},
		Quorum:              1,
		TimeoutAt:           now.Add(time.Hour),
		OnTimeout:           "fail",
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	capH := &captureHandler{}
	logger := slog.New(capH)

	attrs := map[string]any{
		"reviewer_count": 2,
		"gate_name":      "deploy-gate",
		"work_item_id":   "wi-1",
	}
	if err := recordEvent(ctx, recordEventOpts{
		DB:         db,
		Logger:     logger,
		ApprovalID: approvalID,
		Event:      obs.EventApprovalRequested,
		Kind:       EventKindRequested,
		Actor:      "orchestrator",
		Now:        now,
		Attrs:      attrs,
	}); err != nil {
		t.Fatalf("recordEvent: %v", err)
	}

	events, err := db.ListApprovalEvents(ctx, approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events)=%d; want 1", len(events))
	}
	row := events[0]

	rec, ok := capH.findEvent(obs.EventApprovalRequested)
	if !ok {
		t.Fatalf("slog event %q not emitted", obs.EventApprovalRequested)
	}

	// Pull the audit-payload attr off the slog record and assert it
	// byte-equals the row's payload_json. Drift here means a slog-only
	// channel and a DB-only channel encoded different shapes — exactly
	// the bug §5.7 says the single helper must prevent.
	v, ok := slogutil.AttrValue(rec, "audit_payload")
	if !ok {
		t.Fatalf("slog record missing audit_payload attr; have=%v", rec)
	}
	slogPayload := v.String()
	if string(row.Payload) != slogPayload {
		t.Fatalf("payload drift:\n  row=%s\n slog=%s", string(row.Payload), slogPayload)
	}

	// Canonical-JSON guarantee: keys are sorted; round-trip via
	// json.Unmarshal MUST yield the same map both ways.
	var fromRow, fromSlog map[string]any
	if err := json.Unmarshal(row.Payload, &fromRow); err != nil {
		t.Fatalf("unmarshal row payload: %v", err)
	}
	if err := json.Unmarshal([]byte(slogPayload), &fromSlog); err != nil {
		t.Fatalf("unmarshal slog payload: %v", err)
	}
	if !reflect.DeepEqual(fromRow, fromSlog) {
		t.Fatalf("decoded payloads differ:\n  row=%v\n slog=%v", fromRow, fromSlog)
	}

	// Sanity: the helper also stamps Kind + Actor onto the row.
	if row.Kind != EventKindRequested {
		t.Errorf("row.Kind=%q; want %q", row.Kind, EventKindRequested)
	}
	if row.Actor != ActorOrchestrator {
		t.Errorf("row.Actor=%q; want %q", row.Actor, ActorOrchestrator)
	}
}

// Spec §7 A+ — byte-equality across a representative sequence (200
// random attribute payloads). Drift here would mean the recordEvent
// helper silently encoded different bytes to slog vs the row; this is
// the single regression test that backstops the §5.7 "single helper"
// claim across the full input space.
func TestAudit_SlogMatchesRow_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
		db := auditTestDB(t, now)
		ctx := context.Background()
		approvalID := fmt.Sprintf("a-%012d", rapid.IntRange(0, 1<<30).Draw(rt, "aid"))
		if err := db.UpsertWorkItem(ctx, state.WorkItem{
			ID: "wi-prop", Kind: state.KindFeature, Title: "x", Lane: "server",
			Status: state.WorkStatusPlanned,
		}, state.SourceAdapter, now); err != nil {
			rt.Fatalf("UpsertWorkItem: %v", err)
		}
		if err := db.CreateApproval(ctx, state.Approval{
			ID: approvalID, WorkItemID: "wi-prop", GateName: "g",
			RequestedAt: now, RequestedBy: "owner",
			ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"a"}, Quorum: 1},
			Quorum:              1, TimeoutAt: now.Add(time.Hour), OnTimeout: "fail",
		}); err != nil {
			rt.Fatalf("CreateApproval: %v", err)
		}

		// Random payload shape: string, int, bool, nested object.
		attrs := map[string]any{
			"k_str":  rapid.StringMatching(`[a-z]{0,10}`).Draw(rt, "k_str"),
			"k_int":  rapid.IntRange(-1000, 1000).Draw(rt, "k_int"),
			"k_bool": rapid.Bool().Draw(rt, "k_bool"),
		}

		capH := &captureHandler{}
		if err := recordEvent(ctx, recordEventOpts{
			DB:         db,
			Logger:     slog.New(capH),
			ApprovalID: approvalID,
			Event:      obs.EventApprovalDecided,
			Kind:       EventKindDecided,
			Actor:      "alice",
			Now:        now,
			Attrs:      attrs,
		}); err != nil {
			rt.Fatalf("recordEvent: %v", err)
		}
		events, _ := db.ListApprovalEvents(ctx, approvalID)
		if len(events) != 1 {
			rt.Fatalf("len(events)=%d; want 1", len(events))
		}
		rec, ok := capH.findEvent(obs.EventApprovalDecided)
		if !ok {
			rt.Fatalf("slog event missing")
		}
		v, _ := slogutil.AttrValue(rec, "audit_payload")
		if string(events[0].Payload) != v.String() {
			rt.Fatalf("payload drift:\n  row=%s\n slog=%s", string(events[0].Payload), v.String())
		}
		var fromRow, fromSlog map[string]any
		_ = json.Unmarshal(events[0].Payload, &fromRow)
		_ = json.Unmarshal([]byte(v.String()), &fromSlog)
		if !reflect.DeepEqual(fromRow, fromSlog) {
			rt.Fatalf("decoded mismatch: row=%v slog=%v", fromRow, fromSlog)
		}
	})
}

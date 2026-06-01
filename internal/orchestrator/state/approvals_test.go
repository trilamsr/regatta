package state

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func seedApprovalWorkItem(t *testing.T, db *DB, id string) {
	t.Helper()
	item := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: WorkStatusPlanned}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkItem(context.Background(), item, SourceBrief, at); err != nil {
		t.Fatalf("seedApprovalWorkItem(%s): %v", id, err)
	}
}

func sampleApproval(id, wi string, requestedAt, timeoutAt time.Time) Approval {
	return Approval{
		ID:          id,
		WorkItemID:  wi,
		GateName:    "ship-gate",
		RequestedAt: requestedAt,
		RequestedBy: "system",
		ReviewerSetSnapshot: ReviewerSet{
			Reviewers:         []string{"alice", "bob"},
			Quorum:            2,
			PreventSelfReview: true,
		},
		Quorum:    2,
		Status:    "pending",
		TimeoutAt: timeoutAt,
		OnTimeout: "fail",
		EscalationChain: []TierConfig{{
			Reviewers:      []string{"carol"},
			Quorum:         1,
			Timeout:        time.Hour,
			DecisionWindow: 30 * time.Minute,
		}},
	}
}

func TestApprovalMigration_TablesExist(t *testing.T) {
	db := newTestDB(t)
	for _, table := range []string{"approvals", "approval_events"} {
		var name string
		if err := db.SQL().QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %q missing after migrate: %v", table, err)
		}
	}
	for _, idx := range []string{
		"idx_approvals_status",
		"idx_approvals_timeout",
		"idx_approval_events_approval",
		"uq_approval_events_token_consume",
	} {
		var name string
		if err := db.SQL().QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("index %q missing after migrate: %v", idx, err)
		}
	}
}

func TestApproval_CreateGetRoundTrip(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")

	a := sampleApproval("a-0000000001", "F-1", t0, t0.Add(2*time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	got, err := db.GetApproval(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.ID != a.ID || got.WorkItemID != a.WorkItemID || got.GateName != a.GateName {
		t.Fatalf("identity mismatch: got=%+v want=%+v", got, a)
	}
	if got.RequestedBy != "system" || got.Quorum != 2 || got.Status != "pending" {
		t.Fatalf("scalar mismatch: %+v", got)
	}
	if !got.RequestedAt.Equal(t0) {
		t.Fatalf("RequestedAt=%v want %v", got.RequestedAt, t0)
	}
	if !got.TimeoutAt.Equal(t0.Add(2 * time.Hour)) {
		t.Fatalf("TimeoutAt=%v", got.TimeoutAt)
	}
	if len(got.ReviewerSetSnapshot.Reviewers) != 2 || got.ReviewerSetSnapshot.Quorum != 2 || !got.ReviewerSetSnapshot.PreventSelfReview {
		t.Fatalf("ReviewerSet round-trip lost data: %+v", got.ReviewerSetSnapshot)
	}
	if len(got.EscalationChain) != 1 || got.EscalationChain[0].Reviewers[0] != "carol" || got.EscalationChain[0].Timeout != time.Hour {
		t.Fatalf("EscalationChain round-trip lost data: %+v", got.EscalationChain)
	}
	if !got.CreatedAt.Equal(t0) || !got.UpdatedAt.Equal(t0) {
		t.Fatalf("created/updated=%v/%v want %v", got.CreatedAt, got.UpdatedAt, t0)
	}
}

func TestApproval_GetForWorkItem_NilWhenAbsent(t *testing.T) {
	db := newTestDB(t)
	got, err := db.GetApprovalForWorkItem(context.Background(), "F-missing", "ship-gate")
	if err != nil {
		t.Fatalf("GetApprovalForWorkItem(absent): %v", err)
	}
	if got != nil {
		t.Fatalf("got=%+v want nil", got)
	}
}

func TestApproval_GetForWorkItem_FindsByPair(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-0000000001", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetApprovalForWorkItem(ctx, "F-1", "ship-gate")
	if err != nil || got == nil {
		t.Fatalf("err=%v got=%v", err, got)
	}
	if got.ID != a.ID {
		t.Fatalf("ID=%q want %q", got.ID, a.ID)
	}
	// Different gate slug must not match.
	got, err = db.GetApprovalForWorkItem(ctx, "F-1", "other-gate")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("unrelated gate matched: %+v", got)
	}
}

func TestApproval_AppendEventThenListInIDOrder(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-0000000001", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	events := []ApprovalEvent{
		{ApprovalID: a.ID, Ts: t0, Kind: "requested", Actor: "system", Payload: json.RawMessage(`{"reason":"first"}`)},
		{ApprovalID: a.ID, Ts: t0.Add(time.Second), Kind: "notified", Actor: "system"},
		{ApprovalID: a.ID, Ts: t0.Add(2 * time.Second), Kind: "decided", Actor: "alice", Payload: json.RawMessage(`{"decision":"allow"}`)},
	}
	for _, ev := range events {
		if err := db.AppendApprovalEvent(ctx, ev); err != nil {
			t.Fatalf("AppendApprovalEvent(%s): %v", ev.Kind, err)
		}
	}
	got, err := db.ListApprovalEvents(ctx, a.ID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].Kind != "requested" || got[1].Kind != "notified" || got[2].Kind != "decided" {
		t.Fatalf("not id-ASC: %+v", got)
	}
	if got[0].ID >= got[1].ID || got[1].ID >= got[2].ID {
		t.Fatalf("ids not monotonic: %d,%d,%d", got[0].ID, got[1].ID, got[2].ID)
	}
	if got[2].Actor != "alice" {
		t.Fatalf("actor=%q", got[2].Actor)
	}
	if string(got[2].Payload) != `{"decision":"allow"}` {
		t.Fatalf("payload=%q", string(got[2].Payload))
	}
}

func TestApproval_TokenConsumeUniqueBlocksReplay(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-0000000001", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	consume := ApprovalEvent{
		ApprovalID: a.ID, Ts: t0, Kind: "token_consumed", Actor: "alice", TokenJTI: "jti-XYZ",
	}
	if err := db.AppendApprovalEvent(ctx, consume); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	err := db.AppendApprovalEvent(ctx, consume)
	if !errors.Is(err, ErrTokenReplay) {
		t.Fatalf("err=%v want ErrTokenReplay", err)
	}
}

func TestApproval_ListPendingFiltersTerminal(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-A")
	seedApprovalWorkItem(t, db, "F-B")
	seedApprovalWorkItem(t, db, "F-C")
	pending := sampleApproval("a-pending", "F-A", t0, t0.Add(time.Hour))
	approved := sampleApproval("a-appr", "F-B", t0, t0.Add(time.Hour))
	rejected := sampleApproval("a-rej", "F-C", t0, t0.Add(time.Hour))
	for _, a := range []Approval{pending, approved, rejected} {
		if err := db.CreateApproval(ctx, a); err != nil {
			t.Fatalf("CreateApproval(%s): %v", a.ID, err)
		}
	}
	if err := db.MarkApprovalDecided(ctx, "a-appr", "approved", []string{"alice", "bob"}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkApprovalDecided(ctx, "a-rej", "rejected", []string{"alice"}, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListPendingApprovals(ctx)
	if err != nil {
		t.Fatalf("ListPendingApprovals: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a-pending" {
		t.Fatalf("got %+v want exactly [a-pending]", got)
	}
}

func TestApproval_ListTimedOutBefore_FiltersPendingAndDeadline(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-A")
	seedApprovalWorkItem(t, db, "F-B")
	seedApprovalWorkItem(t, db, "F-C")
	seedApprovalWorkItem(t, db, "F-D")

	// expired + pending: included
	exp := sampleApproval("a-exp", "F-A", t0, t0.Add(-time.Minute))
	// future + pending: excluded by deadline
	fut := sampleApproval("a-fut", "F-B", t0, t0.Add(time.Hour))
	// expired but already approved: excluded by status filter
	expDone := sampleApproval("a-expdone", "F-C", t0, t0.Add(-time.Minute))
	// expired but rejected: excluded by status filter
	expRej := sampleApproval("a-exprej", "F-D", t0, t0.Add(-time.Minute))
	for _, a := range []Approval{exp, fut, expDone, expRej} {
		if err := db.CreateApproval(ctx, a); err != nil {
			t.Fatalf("CreateApproval(%s): %v", a.ID, err)
		}
	}
	if err := db.MarkApprovalDecided(ctx, "a-expdone", "approved", []string{"alice"}, t0); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkApprovalDecided(ctx, "a-exprej", "rejected", []string{"alice"}, t0); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListApprovalsTimedOutBefore(ctx, t0)
	if err != nil {
		t.Fatalf("ListApprovalsTimedOutBefore: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a-exp" {
		t.Fatalf("got %+v want exactly [a-exp]", got)
	}
}

func TestApproval_MarkDecided_WritesDenormColumns(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-0000000001", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	decidedAt := t0.Add(5 * time.Minute)
	if err := db.MarkApprovalDecided(ctx, a.ID, "approved", []string{"alice", "bob"}, decidedAt); err != nil {
		t.Fatalf("MarkApprovalDecided: %v", err)
	}
	got, err := db.GetApproval(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "approved" {
		t.Fatalf("Status=%q want approved", got.Status)
	}
	if !got.DecidedAt.Equal(decidedAt) {
		t.Fatalf("DecidedAt=%v want %v", got.DecidedAt, decidedAt)
	}
	if len(got.DecidedBy) != 2 || got.DecidedBy[0] != "alice" || got.DecidedBy[1] != "bob" {
		t.Fatalf("DecidedBy=%v", got.DecidedBy)
	}
}

func TestApproval_ActorCheckRejectsInvalid(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-0000000001", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		actor string
	}{
		{"newline", "alice\nbob"},
		{"control_char", "alice\x07"},
		{"space", "alice bob"},
		{"over_128_chars", strings.Repeat("a", 129)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := db.AppendApprovalEvent(ctx, ApprovalEvent{
				ApprovalID: a.ID, Ts: t0, Kind: "decided", Actor: tc.actor,
			})
			if err == nil {
				t.Fatalf("actor=%q accepted; want CHECK constraint error", tc.actor)
			}
		})
	}

	// Permitted actor passes — pins that the constraint is not over-strict.
	if err := db.AppendApprovalEvent(ctx, ApprovalEvent{
		ApprovalID: a.ID, Ts: t0, Kind: "decided", Actor: "system:timeout-default",
	}); err != nil {
		t.Fatalf("legitimate actor rejected: %v", err)
	}
}

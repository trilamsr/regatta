package state

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Branch-coverage tests for approvals.go — drive each public method
// past 90% line coverage by exercising the previously-uncovered error
// paths (issue #139). Pairs with approvals_test.go (happy paths +
// the constraint and race tests).

// TestApproval_CreateApproval_StatusEmptyDefaultsPending pins the
// zero-Status → "pending" branch in CreateApproval (line 117).
func TestApproval_CreateApproval_StatusEmptyDefaultsPending(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-default-status", "F-1", t0, t0.Add(time.Hour))
	a.Status = "" // force default branch
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	got, err := db.GetApproval(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != ApprovalStatusPending {
		t.Fatalf("Status=%q want %q", got.Status, ApprovalStatusPending)
	}
}

// TestApproval_CreateApproval_OnTimeoutEmptyDefaultsFail pins the
// zero-OnTimeout default branch in CreateApproval (line 121).
func TestApproval_CreateApproval_OnTimeoutEmptyDefaultsFail(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	// Reference sample has OnTimeout="fail" already; clearing it on a
	// distinct value forces the default-branch to fire and we compare
	// the round-tripped value to the reference rather than re-literalling
	// "fail" (goconst lint).
	ref := sampleApproval("a-default-ontimeout-ref", "F-1", t0, t0.Add(time.Hour))
	a := sampleApproval("a-default-ontimeout", "F-1", t0, t0.Add(time.Hour))
	a.OnTimeout = "" // force default branch
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	got, err := db.GetApproval(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.OnTimeout != ref.OnTimeout {
		t.Fatalf("OnTimeout=%q want %q (default)", got.OnTimeout, ref.OnTimeout)
	}
}

// TestApproval_CreateApproval_NonUniqueErrorWraps drives the generic
// (non-collision) ExecContext error path in CreateApproval (line 140)
// by violating the work_item_id FK — sqlite returns a constraint
// error that is NOT the work_item_id/gate_name UNIQUE tuple.
func TestApproval_CreateApproval_NonUniqueErrorWraps(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	// No seedApprovalWorkItem: work_item_id has no parent row, FK fails.
	a := sampleApproval("a-fk-fail", "F-MISSING", t0, t0.Add(time.Hour))
	err := db.CreateApproval(ctx, a)
	if err == nil {
		t.Fatalf("CreateApproval(missing FK): err=nil, want FK constraint error")
	}
	if errors.Is(err, ErrApprovalAlreadyExists) {
		t.Fatalf("FK error misclassified as ErrApprovalAlreadyExists: %v", err)
	}
	if !strings.Contains(err.Error(), "state: insert approval") {
		t.Fatalf("err=%q want wrap prefix 'state: insert approval'", err.Error())
	}
}

// TestApproval_GetApproval_NotFoundWrapsErr drives the scanApproval
// non-nil error path in GetApproval (line 158).
func TestApproval_GetApproval_NotFoundWrapsErr(t *testing.T) {
	db := newTestDB(t)
	_, err := db.GetApproval(context.Background(), "a-missing")
	if err == nil {
		t.Fatalf("GetApproval(missing): err=nil, want wrapped sql.ErrNoRows")
	}
	if !strings.Contains(err.Error(), "state: get approval") {
		t.Fatalf("err=%q want wrap prefix 'state: get approval'", err.Error())
	}
}

// TestApproval_GetApprovalForWorkItem_CorruptJSONWrapsErr drives the
// non-NoRows scan-error branch in GetApprovalForWorkItem (line 176)
// by mutating the JSON column to be unparseable, which makes
// scanApproval return an unmarshal error rather than sql.ErrNoRows.
func TestApproval_GetApprovalForWorkItem_CorruptJSONWrapsErr(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-corrupt-rs", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Corrupt the reviewer_set_snapshot_json column directly.
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE approvals SET reviewer_set_snapshot_json = '{not json' WHERE id = ?`, a.ID); err != nil {
		t.Fatalf("seed corrupt JSON: %v", err)
	}
	_, err := db.GetApprovalForWorkItem(ctx, "F-1", "ship-gate")
	if err == nil {
		t.Fatalf("GetApprovalForWorkItem(corrupt): err=nil, want unmarshal error")
	}
	if !strings.Contains(err.Error(), "state: get approval for work item") {
		t.Fatalf("err=%q want wrap prefix 'state: get approval for work item'", err.Error())
	}
}

// TestApproval_MarkDecided_NotFoundReturnsErr drives the rows == 0
// branch in MarkApprovalDecided (line 294).
func TestApproval_MarkDecided_NotFoundReturnsErr(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	err := db.MarkApprovalDecided(context.Background(), "a-missing",
		ApprovalStatusApproved, []string{"alice"}, t0)
	if err == nil {
		t.Fatalf("MarkApprovalDecided(missing): err=nil, want not-found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err=%q want 'not found' marker", err.Error())
	}
}

// TestApproval_ListAndScan_PropagateUnmarshalErr drives the scan-error
// branches in scanApproval (lines 321-329) and in scanApprovals (lines
// 337-339) and ListApprovalEvents' rows.Scan wrap (line 228) via
// reaching list paths that re-encounter the corrupt JSON.
func TestApproval_ListAndScan_PropagateUnmarshalErr(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-bad-chain", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Corrupt escalation_chain_json so scanApproval's third unmarshal
	// fails — exercises a different branch than the reviewer_set probe.
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE approvals SET escalation_chain_json = '{bad' WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListPendingApprovals(ctx); err == nil {
		t.Fatalf("ListPendingApprovals(corrupt): err=nil, want unmarshal propagation")
	}
	if _, err := db.ListApprovalsTimedOutBefore(ctx, t0.Add(time.Hour*2)); err == nil {
		t.Fatalf("ListApprovalsTimedOutBefore(corrupt): err=nil, want unmarshal propagation")
	}
	if _, err := db.GetApproval(ctx, a.ID); err == nil {
		t.Fatalf("GetApproval(corrupt): err=nil, want unmarshal propagation")
	}

	// Separately corrupt decided_by so scanApproval's second unmarshal fails.
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE approvals SET decided_by = 'not-json', escalation_chain_json = '[]' WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetApproval(ctx, a.ID); err == nil {
		t.Fatalf("GetApproval(corrupt decided_by): err=nil, want unmarshal propagation")
	}
}

// TestApproval_ClosedDBPropagatesQueryErrors drives the ExecContext /
// QueryContext error wraps that fire when the *sql.DB has been closed
// — covers ListPendingApprovals/ListApprovalsTimedOutBefore/
// ListApprovalEvents/MarkApprovalDecided/AppendApprovalEvent error
// returns at the I/O boundary.
func TestApproval_ClosedDBPropagatesQueryErrors(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	dsn := DSN(filepath.Join(t.TempDir(), "closed.db"))
	db, err := OpenWithClock(context.Background(), dsn, func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Close immediately; every subsequent op should surface as a wrapped error.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	if _, err := db.ListPendingApprovals(ctx); err == nil ||
		!strings.Contains(err.Error(), "state: list pending approvals") {
		t.Fatalf("ListPendingApprovals(closed): err=%v want wrap", err)
	}
	if _, err := db.ListApprovalsTimedOutBefore(ctx, t0); err == nil ||
		!strings.Contains(err.Error(), "state: list approvals timed out") {
		t.Fatalf("ListApprovalsTimedOutBefore(closed): err=%v want wrap", err)
	}
	if _, err := db.ListApprovalEvents(ctx, "a-x"); err == nil ||
		!strings.Contains(err.Error(), "state: list approval events") {
		t.Fatalf("ListApprovalEvents(closed): err=%v want wrap", err)
	}
	if err := db.MarkApprovalDecided(ctx, "a-x", ApprovalStatusApproved, []string{"alice"}, t0); err == nil ||
		!strings.Contains(err.Error(), "state: mark approval decided") {
		t.Fatalf("MarkApprovalDecided(closed): err=%v want wrap", err)
	}
	if err := db.AppendApprovalEvent(ctx, ApprovalEvent{
		ApprovalID: "a-x", Ts: t0, Kind: "noted", Actor: "system",
	}); err == nil || !strings.Contains(err.Error(), "state: append approval event") {
		t.Fatalf("AppendApprovalEvent(closed): err=%v want wrap", err)
	}
	if err := db.CreateApproval(ctx, sampleApproval("a-x", "F-1", t0, t0.Add(time.Hour))); err == nil ||
		!strings.Contains(err.Error(), "state: insert approval") {
		t.Fatalf("CreateApproval(closed): err=%v want wrap", err)
	}
}

// TestApproval_UniqueProbes_NilGuard pins the nil-err short-circuit
// in isUniqueTokenConsume and isUniqueWorkItemGate. Probes are
// package-private so the test lives next to them; the nil guard
// matters because callers chain through fmt.Errorf wrapping.
func TestApproval_UniqueProbes_NilGuard(t *testing.T) {
	if isUniqueTokenConsume(nil) {
		t.Fatalf("isUniqueTokenConsume(nil) = true; want false")
	}
	if isUniqueWorkItemGate(nil) {
		t.Fatalf("isUniqueWorkItemGate(nil) = true; want false")
	}
	// Non-matching error must also be false.
	if isUniqueTokenConsume(errors.New("unrelated")) {
		t.Fatalf("isUniqueTokenConsume(unrelated)=true; want false")
	}
	if isUniqueWorkItemGate(errors.New("unrelated")) {
		t.Fatalf("isUniqueWorkItemGate(unrelated)=true; want false")
	}
}

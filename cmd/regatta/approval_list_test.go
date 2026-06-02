package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newApprovalListHarness(t *testing.T) (*state.DB, string, time.Time) {
	t.Helper()
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	dbPath := filepath.Join(t.TempDir(), "list.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, state.DSN(dbPath), t0
}

func runList(t *testing.T, dsn string, clock func() time.Time, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runApprovalListWith(approvalListDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: clock, DSN: dsn,
	}, args)
	return code, stdout.String(), stderr.String()
}

func TestApprovalList_EmptyExits0WithNoApprovalsMessage(t *testing.T) {
	_, dsn, t0 := newApprovalListHarness(t)
	clock := func() time.Time { return t0 }
	code, stdout, stderr := runList(t, dsn, clock)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(strings.ToLower(stdout), "no approvals") {
		t.Fatalf("expected 'no approvals' message, got stdout=%q", stdout)
	}
}

func TestApprovalList_MineFilter(t *testing.T) {
	db, dsn, t0 := newApprovalListHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	for _, id := range []string{"F-A", "F-B"} {
		seedApprovalWorkItemForCLI(t, db, id, t0)
	}
	mine := state.Approval{
		ID: "a-mine", WorkItemID: "F-A", GateName: "ship-gate",
		RequestedAt: t0, RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 1},
		Quorum: 1, Status: state.ApprovalStatusPending,
		TimeoutAt: t0.Add(time.Hour), OnTimeout: "fail",
	}
	other := state.Approval{
		ID: "a-other", WorkItemID: "F-B", GateName: "ship-gate",
		RequestedAt: t0, RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"carol"}, Quorum: 1},
		Quorum: 1, Status: state.ApprovalStatusPending,
		TimeoutAt: t0.Add(time.Hour), OnTimeout: "fail",
	}
	if err := db.CreateApproval(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateApproval(ctx, other); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runList(t, dsn, clock, "--mine", "alice", "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("json.Unmarshal: %v stdout=%q", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("--mine alice rows=%d want 1: %v", len(rows), rows)
	}
	if rows[0]["approval_id"] != "a-mine" {
		t.Fatalf("approval_id=%v want a-mine", rows[0]["approval_id"])
	}
}

func TestApprovalList_JSONShape(t *testing.T) {
	db, dsn, t0 := newApprovalListHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	seedApprovalWorkItemForCLI(t, db, "F-1", t0)
	a := state.Approval{
		ID: "a-json01", WorkItemID: "F-1", GateName: "ship-gate",
		RequestedAt: t0, RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 2},
		Quorum: 2, Status: state.ApprovalStatusPending,
		TimeoutAt: t0.Add(2 * time.Hour), OnTimeout: "fail",
	}
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runList(t, dsn, clock, "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	want := []string{"approval_id", "work_item_id", "gate_name", "requested_at_unix", "timeout_at_unix", "reviewer_set", "quorum"}
	for _, k := range want {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("missing key %q in %v", k, rows[0])
		}
	}
}

// TestApprovalList_JSONMatchesSchema validates every emitted row against the embedded JSON Schema contract.
func TestApprovalList_JSONMatchesSchema(t *testing.T) {
	db, dsn, t0 := newApprovalListHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	seedApprovalWorkItemForCLI(t, db, "F-1", t0)
	a := state.Approval{
		ID: "a-schema1", WorkItemID: "F-1", GateName: "ship-gate",
		RequestedAt: t0, RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 2},
		Quorum: 2, Status: state.ApprovalStatusPending,
		TimeoutAt: t0.Add(2 * time.Hour), OnTimeout: "fail",
	}
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runList(t, dsn, clock, "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}

	// Compile the embedded schema once.
	schemaDoc, err := jsonschema.UnmarshalJSON(strings.NewReader(schemas.ApprovalListRowSchemaJSON))
	if err != nil {
		t.Fatalf("UnmarshalJSON schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemas.ApprovalListRowSchemaID, schemaDoc); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	sch, err := c.Compile(schemas.ApprovalListRowSchemaID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Validate each row individually against the row schema.
	var rows []any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected >=1 row, got 0")
	}
	for i, row := range rows {
		if err := sch.Validate(row); err != nil {
			t.Errorf("row[%d] failed schema: %v\nrow=%v", i, err, row)
		}
	}
}

func TestApprovalList_TableFormatColumns(t *testing.T) {
	db, dsn, t0 := newApprovalListHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	seedApprovalWorkItemForCLI(t, db, "F-1", t0)
	a := state.Approval{
		ID: "a-table1", WorkItemID: "F-1", GateName: "ship-gate",
		RequestedAt: t0, RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 2},
		Quorum: 2, Status: state.ApprovalStatusPending,
		TimeoutAt: t0.Add(2 * time.Hour), OnTimeout: "fail",
	}
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runList(t, dsn, clock, "--format", "table")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	for _, col := range []string{"APPROVAL_ID", "WORK_ITEM_ID", "GATE_NAME", "REQUESTED_AT", "TIMEOUT_AT", "REVIEWERS", "QUORUM"} {
		if !strings.Contains(stdout, col) {
			t.Errorf("missing column %q in table output: %q", col, stdout)
		}
	}
	// Header + 1 row + (optional trailing newline) — at minimum 2 lines.
	gotLines := strings.Count(strings.TrimRight(stdout, "\n"), "\n") + 1
	if gotLines < 2 {
		t.Fatalf("table line count=%d want >=2", gotLines)
	}
}

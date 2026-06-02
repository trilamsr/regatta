package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestApprovalListSchemaLockstep pins Go ↔ JSON-Schema field parity for ApprovalListRow.
func TestApprovalListSchemaLockstep(t *testing.T) {
	t.Parallel()
	row := ApprovalListRow{
		ApprovalID:      "a-lockstep01",
		WorkItemID:      "F-1",
		GateName:        "ship-gate",
		RequestedAtUnix: 1717200000,
		TimeoutAtUnix:   1717207200,
		ReviewerSet:     []string{"alice", "bob"},
		Quorum:          2,
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Every required key in the schema MUST appear in the canonical
	// Go form — drift in either direction is a contract break.
	for _, req := range []string{
		`"approval_id"`, `"work_item_id"`, `"gate_name"`,
		`"requested_at_unix"`, `"timeout_at_unix"`,
		`"reviewer_set"`, `"quorum"`,
	} {
		if !strings.Contains(string(raw), req) {
			t.Fatalf("missing %s in canonical form: %s", req, raw)
		}
	}

	// And the embedded schema must declare exactly the same required
	// set — guards against a Go-side rename without a schema bump.
	var schemaDoc struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(ApprovalListRowSchemaJSON), &schemaDoc); err != nil {
		t.Fatalf("Unmarshal embedded schema: %v", err)
	}
	wantRequired := map[string]bool{
		"approval_id": true, "work_item_id": true, "gate_name": true,
		"requested_at_unix": true, "timeout_at_unix": true,
		"reviewer_set": true, "quorum": true,
	}
	if len(schemaDoc.Required) != len(wantRequired) {
		t.Fatalf("schema required len=%d want %d: %v", len(schemaDoc.Required), len(wantRequired), schemaDoc.Required)
	}
	for _, k := range schemaDoc.Required {
		if !wantRequired[k] {
			t.Errorf("unexpected required key in schema: %q", k)
		}
	}
}

// TestApprovalListRowSchemaIDMatchesEmbeddedJSON pins the exported $id constant against the embedded schema.
func TestApprovalListRowSchemaIDMatchesEmbeddedJSON(t *testing.T) {
	t.Parallel()
	var schemaDoc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal([]byte(ApprovalListRowSchemaJSON), &schemaDoc); err != nil {
		t.Fatalf("Unmarshal embedded schema: %v", err)
	}
	if schemaDoc.ID != ApprovalListRowSchemaID {
		t.Fatalf("$id=%q want %q", schemaDoc.ID, ApprovalListRowSchemaID)
	}
}

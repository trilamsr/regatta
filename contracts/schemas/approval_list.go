package schemas

import _ "embed"

// ApprovalListRow is the contract surface for one row of
// `regatta approval list --format=json`. Matches
// approval_list.v1.json field-for-field.
//
// Schema-lockstep discipline: any field added here must also be
// added to approval_list.v1.json (and vice versa).
// TestApprovalListSchemaLockstep in approval_list_test.go
// round-trips a fixture through both shapes and fails on drift.
// TestApprovalList_JSONMatchesSchema in cmd/regatta validates
// actual CLI output against the embedded schema so a column drift
// fails at schema-check time, not at downstream-tool runtime.
type ApprovalListRow struct {
	ApprovalID      string   `json:"approval_id"`
	WorkItemID      string   `json:"work_item_id"`
	GateName        string   `json:"gate_name"`
	RequestedAtUnix int64    `json:"requested_at_unix"`
	TimeoutAtUnix   int64    `json:"timeout_at_unix"`
	ReviewerSet     []string `json:"reviewer_set"`
	Quorum          int      `json:"quorum"`
}

// ApprovalListRowSchemaJSON is the raw JSON Schema for one row of
// `regatta approval list --format=json`, embedded so cmd/regatta and
// downstream-tool consumers can validate without a filesystem
// dependency at runtime.
//
//go:embed approval_list.v1.json
var ApprovalListRowSchemaJSON string

// ApprovalListRowSchemaID is the canonical $id of the row schema —
// the URL callers MUST use when registering the embedded bytes with
// jsonschema.Compiler.AddResource. Keeping it as a constant prevents
// drift between the schema file's "$id" and the URL strings scattered
// across callers.
const ApprovalListRowSchemaID = "https://github.com/trilamsr/regatta/schemas/approval_list.v1.json"

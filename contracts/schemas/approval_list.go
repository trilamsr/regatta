package schemas

import _ "embed"

// ApprovalListRow is the contract surface for one row of
// `regatta approval list --format=json`. Schema-lockstep with
// approval_list.v1.json; TestApprovalListSchemaLockstep round-trips a
// fixture through both shapes and fails on drift.
type ApprovalListRow struct {
	ApprovalID      string   `json:"approval_id"`
	WorkItemID      string   `json:"work_item_id"`
	GateName        string   `json:"gate_name"`
	RequestedAtUnix int64    `json:"requested_at_unix"`
	TimeoutAtUnix   int64    `json:"timeout_at_unix"`
	ReviewerSet     []string `json:"reviewer_set"`
	Quorum          int      `json:"quorum"`
}

// ApprovalListRowSchemaJSON is the raw JSON Schema embedded so cmd/regatta and downstream consumers validate without a filesystem dep.
//
//go:embed approval_list.v1.json
var ApprovalListRowSchemaJSON string

// ApprovalListRowSchemaID is the canonical $id of the row schema — the
// URL callers MUST use when registering the embedded bytes with
// jsonschema.Compiler.AddResource. Constant prevents drift between the
// schema file's "$id" and caller URL strings.
const ApprovalListRowSchemaID = "https://github.com/trilamsr/regatta/schemas/approval_list.v1.json"

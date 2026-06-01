package spend

// ScopeKind enumerates the scopes a cost cap may apply to. Mirrors
// the CUE config block: per_dag_usd / per_operator_usd /
// per_work_item_usd / global. Spec §3.5.
type ScopeKind string

const (
	// ScopeDAG filters spend by payload.dag_id.
	ScopeDAG ScopeKind = "dag"
	// ScopeOperator filters spend by payload.operator_id.
	ScopeOperator ScopeKind = "operator"
	// ScopeWorkItem filters spend by payload.work_item_id.
	ScopeWorkItem ScopeKind = "work_item"
	// ScopeGlobal sums every token_spend row for the tenant.
	ScopeGlobal ScopeKind = "global"
)

// ScopeKey carries the resolved filter values for one BudgetState
// query. TenantID is always required (substrate.DefaultTenantID until
// W8) — R9 forward-fit.
type ScopeKey struct {
	Kind       ScopeKind
	DAGID      string
	OperatorID string
	WorkItemID string
	TenantID   string
}

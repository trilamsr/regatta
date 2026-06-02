// Package authz owns the Authorizer seam — every gated handler routes
// through Check(ctx, principal, action, resource) before mutating state.
//
// Interface born with TWO callers (web handler + CLI re-spawn) per spec
// §3.1 — closes the W7 R4 "premature interface" deferral.
package authz

import (
	"context"
	"errors"

	"github.com/trilamsr/regatta/internal/web"
)

// Action enumerates gated operations evaluated by OPA. Spec §3.1.
type Action string

// Spec §3.1 — 4 actions verbatim. SQL CHECK / Rego data-key parity is
// the load-bearing invariant; do NOT rename without re-spawning design.
const (
	ActionApprovalView   Action = "approval.view"
	ActionApprovalDecide Action = "approval.decide"
	ActionRunView        Action = "run.view"
	ActionRunCostView    Action = "run.cost.view"
)

// Resource is the opaque target — approval ULID, run ULID, or
// "tenant:<id>" for tenant-scoped ops. OPA inspects via input.resource.
type Resource string

// Decision is what OPA renders. Reason is the Rego policy's `reason`
// binding — safe to surface in audit log + UI; not raw policy text.
// PolicyRevision is the 8-char bundle SHA prefix (spec §3.7 R7 — full
// SHA stays in the audit row to avoid OTel cardinality blowup).
type Decision struct {
	Allow          bool
	Reason         string
	PolicyRevision string
}

// Authorizer is the seam consumed by web handlers and the CLI
// approve-decide path. Both callers exist at birth (spec §3.1 line 96).
type Authorizer interface {
	Check(ctx context.Context, p web.Principal, a Action, r Resource) (Decision, error)
}

// Sentinels — typed errors so callers errors.Is-match without string
// compare. Naming verbatim from spec §3.1.
var (
	ErrDenied          = errors.New("authz: denied")
	ErrTenantUnknown   = errors.New("authz: tenant unknown")
	ErrPolicyMissing   = errors.New("authz: no policy bundle for tenant")
	ErrPolicyEvalError = errors.New("authz: opa eval error")
)

// DefaultTenant is the canonical single-tenant slot — populated when a
// signed payload's Tenant field is empty (spec §3.4.1 wire-back-compat).
const DefaultTenant = "default"

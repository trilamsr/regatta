package authz

import (
	"context"

	"github.com/trilamsr/regatta/internal/web"
)

// ctxKey is unexported so external packages cannot poison the slot.
type ctxKey struct{}

// principalKey is the sole context-key value for the bound Principal.
var principalKey = ctxKey{}

// WithPrincipal attaches p to ctx so deep call paths (e.g. inside
// approval.DecideTx) can re-authorize without plumbing it through
// every signature. Spec §3.1.
func WithPrincipal(ctx context.Context, p web.Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFromContext returns the bound Principal or (zero, false)
// when none is attached. Callers MUST treat the false branch as
// "anonymous" and fail-closed via the Authorizer.
func PrincipalFromContext(ctx context.Context) (web.Principal, bool) {
	p, ok := ctx.Value(principalKey).(web.Principal)
	return p, ok
}

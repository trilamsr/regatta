package authz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/web"
)

// BundleLoader is the seam T2's policies package satisfies (spec §3.3.2
// ActiveBundle signature). T1 ships an inline default loader so this
// package is self-contained; T5 wires the real DB-backed loader at
// serve startup.
type BundleLoader interface {
	ActiveBundle(ctx context.Context, tenant string) (sha string, files map[string]string, err error)
	Tenants(ctx context.Context) ([]string, error)
}

// Config is the constructor input. Empty Config picks safe defaults —
// the inline default-deny baseline bundle is registered for "default".
type Config struct {
	// Loader supplies per-tenant Rego bundles. Nil => inline default
	// loader (spec §3.5 baseline + "default" tenant only).
	Loader BundleLoader
	// Tracer is the OTel tracer used for Check spans (W6 T5 #210
	// normalization). Nil => otel.Tracer("internal/authz").
	Tracer trace.Tracer
}

// OPAAuthorizer is the concrete Authorizer driven by github.com/open-policy-agent/opa/rego.
//
// Hot-path layout (Check): atomic.Pointer.Load => map lookup =>
// PreparedEvalQuery.Eval. No compile on the request path; compile
// runs once at Hydrate + once per Reload (post-commit callback of
// the substrate policy_revision AppendEvent transaction).
type OPAAuthorizer struct {
	store  atomic.Pointer[opaStore]
	loader BundleLoader
	tracer trace.Tracer

	// Test-only revision tracking. Populated by ReloadTenantForTest
	// so TestOpaStore_SwapIsAtomic / *_NoTorn can detect a torn read.
	testRevsMu sync.Mutex
	testRevs   map[string]map[string]struct{}
}

// NewOPAAuthorizer wires the loader + tracer. Hydrate must be called
// before Check; constructor returns no error because all heavy work
// happens in Hydrate.
func NewOPAAuthorizer(cfg Config) (*OPAAuthorizer, error) {
	loader := cfg.Loader
	if loader == nil {
		loader = newDefaultLoader()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("internal/authz")
	}
	a := &OPAAuthorizer{loader: loader, tracer: tracer}
	a.store.Store(&opaStore{
		queries:   map[string]*rego.PreparedEvalQuery{},
		revisions: map[string]string{},
	})
	return a, nil
}

// Hydrate walks every tenant present in the loader and compiles one
// PreparedEvalQuery per (tenant, action). Spec §3.2: compile happens
// at policy-revision write time + boot, NEVER on the eval hot path.
func (a *OPAAuthorizer) Hydrate(ctx context.Context) error {
	tenants, err := a.loader.Tenants(ctx)
	if err != nil {
		return fmt.Errorf("authz: tenants: %w", err)
	}
	if len(tenants) == 0 {
		// Empty deployment still needs the default-deny baseline so
		// single-tenant deploys work out-of-the-box (spec §3.5 + R6).
		tenants = []string{DefaultTenant}
	}
	next := &opaStore{
		queries:   map[string]*rego.PreparedEvalQuery{},
		revisions: map[string]string{},
	}
	for _, tenant := range tenants {
		sha, files, lerr := a.loader.ActiveBundle(ctx, tenant)
		if lerr != nil {
			return fmt.Errorf("authz: load %s: %w", tenant, lerr)
		}
		q4, perr := prepareQueries(ctx, files)
		if perr != nil {
			return fmt.Errorf("authz: prepare %s: %w", tenant, perr)
		}
		rev := shaPrefix(sha)
		next.revisions[tenant] = rev
		for act, q := range q4 {
			next.queries[tenant+"/"+string(act)] = q
		}
	}
	a.store.Store(next)
	return nil
}

// Reload rebuilds the (tenant, action) slot set against the loader's
// current view + atomically swaps the store pointer. Spec §3.3.3 —
// copy-on-write; in-flight evals complete against the old store.
func (a *OPAAuthorizer) Reload(ctx context.Context, tenant string) error {
	sha, files, err := a.loader.ActiveBundle(ctx, tenant)
	if err != nil {
		return fmt.Errorf("authz: reload load %s: %w", tenant, err)
	}
	q4, perr := prepareQueries(ctx, files)
	if perr != nil {
		return fmt.Errorf("authz: reload prepare %s: %w", tenant, perr)
	}
	for {
		cur := a.store.Load()
		next := cur.cloneWithTenant(tenant, shaPrefix(sha), q4)
		if a.store.CompareAndSwap(cur, next) {
			return nil
		}
		// CAS lost a race with a concurrent Reload — retry against
		// the new baseline so neither writer's slot is silently dropped.
	}
}

// Check is the hot path. Spec §3.6 request flow: load *opaStore via
// store.Load(); look up queries[tenant+"/"+action]; run rego.Eval;
// map result -> Decision.
func (a *OPAAuthorizer) Check(ctx context.Context, p web.Principal, act Action, r Resource) (Decision, error) {
	ctx, span := a.tracer.Start(ctx, "authz.Check")
	defer span.End()

	s := a.store.Load()
	q, rev, ok := s.query(p.Tenant, act)
	if !ok {
		return Decision{}, ErrTenantUnknown
	}
	input, err := buildEvalInput(p, act, r)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %w", ErrPolicyEvalError, err)
	}
	rs, err := q.Eval(ctx, rego.EvalParsedInput(input))
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %w", ErrPolicyEvalError, err)
	}
	d := mapResult(rs, rev)
	return d, nil
}

// buildEvalInput converts the Check args into an ast.Value, skipping
// the generic map[string]any -> ast.Value walk OPA would otherwise do
// at every Eval. Reuses pooled string Terms for object keys to cut
// per-call allocations (spec §5 R1 budget; p99 sits near GC noise).
func buildEvalInput(p web.Principal, act Action, r Resource) (ast.Value, error) {
	var roles *ast.Term
	if len(p.Roles) == 0 {
		roles = emptyRoles
	} else {
		ts := make([]*ast.Term, len(p.Roles))
		for i, role := range p.Roles {
			ts[i] = ast.StringTerm(role)
		}
		roles = ast.ArrayTerm(ts...)
	}
	principal := ast.NewObject(
		ast.Item(idTerm, ast.StringTerm(p.ID)),
		ast.Item(tenantTerm, ast.StringTerm(p.Tenant)),
		ast.Item(rolesTerm, roles),
	)
	obj := ast.NewObject(
		ast.Item(principalK, ast.NewTerm(principal)),
		ast.Item(actionK, ast.StringTerm(string(act))),
		ast.Item(resourceK, ast.StringTerm(string(r))),
		ast.Item(nowK, ast.IntNumberTerm(int(time.Now().Unix()))),
	)
	return obj, nil
}

// ReloadTenantForTest installs a synthetic per-revision Rego module for
// the test tenant. It is the load-bearing seam for TestOpaStore_SwapIsAtomic
// + TestOpaStore_ReloadDuringEval_NoTorn — production code uses Reload.
func (a *OPAAuthorizer) ReloadTenantForTest(tenant string, seed int) error {
	files := defaultBundleFiles()
	// Mutate one file's body so each seed renders a distinct SHA — the
	// torn-read test asserts every Check.Decision.PolicyRevision belongs
	// to the known seed set.
	files["regatta/v1/default/marker.rego"] = fmt.Sprintf("package regatta.v1.marker\n\nrev := %d\n", seed)
	sha := bundleSHA(files)
	q4, err := prepareQueries(context.Background(), files)
	if err != nil {
		return err
	}
	a.recordRevisionForTest(tenant, a.store.Load().revisions[tenant])
	a.recordRevisionForTest(tenant, shaPrefix(sha))
	for {
		cur := a.store.Load()
		next := cur.cloneWithTenant(tenant, shaPrefix(sha), q4)
		if a.store.CompareAndSwap(cur, next) {
			return nil
		}
	}
}

// recordRevisionForTest tracks every revision the test installed so the
// assertion phase can detect a torn read.
func (a *OPAAuthorizer) recordRevisionForTest(tenant, rev string) {
	a.testRevsMu.Lock()
	defer a.testRevsMu.Unlock()
	if a.testRevs == nil {
		a.testRevs = map[string]map[string]struct{}{}
	}
	if _, ok := a.testRevs[tenant]; !ok {
		a.testRevs[tenant] = map[string]struct{}{}
	}
	a.testRevs[tenant][rev] = struct{}{}
}

// KnownRevisionsForTest returns the set of SHAs the test installed.
func (a *OPAAuthorizer) KnownRevisionsForTest(tenant string) map[string]struct{} {
	a.testRevsMu.Lock()
	defer a.testRevsMu.Unlock()
	out := map[string]struct{}{}
	for k := range a.testRevs[tenant] {
		out[k] = struct{}{}
	}
	// Always include the live revision so a Check that fires after the
	// last recorded Reload but before this assert reads is recognized.
	out[a.store.Load().revisions[tenant]] = struct{}{}
	return out
}

// prepareQueries compiles one PreparedEvalQuery per spec-mandated
// action. Spec §3.2: query string = data.regatta.v1.<action>.decision.
// Per-action compile keeps the eval graph small — only modules in the
// action's package contribute to its prepared query.
func prepareQueries(ctx context.Context, files map[string]string) (map[Action]*rego.PreparedEvalQuery, error) {
	out := map[Action]*rego.PreparedEvalQuery{}
	for _, act := range []Action{
		ActionApprovalView, ActionApprovalDecide, ActionRunView, ActionRunCostView,
	} {
		q := buildQuery(act)
		opts := make([]func(*rego.Rego), 0, 1+len(files))
		opts = append(opts, rego.Query(q))
		for name, src := range files {
			opts = append(opts, rego.Module(name, src))
		}
		pq, err := rego.New(opts...).PrepareForEval(ctx)
		if err != nil {
			return nil, fmt.Errorf("prepare %s: %w", act, err)
		}
		out[act] = &pq
	}
	return out, nil
}

// stringTermPool keeps the per-Check ast.Term allocations off the heap.
// Eval has the dominant cost; trimming our wrapper allocations buys a
// few µs each call which matters at p99.
var (
	idTerm     = ast.StringTerm("id")
	tenantTerm = ast.StringTerm("tenant")
	rolesTerm  = ast.StringTerm("roles")
	principalK = ast.StringTerm("principal")
	actionK    = ast.StringTerm("action")
	resourceK  = ast.StringTerm("resource")
	nowK       = ast.StringTerm("now_unix")
	emptyRoles = ast.ArrayTerm()
)

// buildQuery returns the OPA query path for an action. Spec §3.2 line 108
// reads "data.regatta.v1.<action>.decision". Action strings carry dots
// (approval.decide, run.cost.view); the path keeps them as nested keys
// so Rego packages map 1:1 (e.g. package regatta.v1.approval.decide).
func buildQuery(a Action) string {
	return "data.regatta.v1." + string(a) + ".decision"
}

// mapResult collapses the OPA ResultSet to a Decision. Missing /
// malformed bindings fail closed.
func mapResult(rs rego.ResultSet, rev string) Decision {
	d := Decision{Allow: false, Reason: "default-deny", PolicyRevision: rev}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return d
	}
	v, ok := rs[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return d
	}
	if a, ok := v["allow"].(bool); ok {
		d.Allow = a
	}
	if r, ok := v["reason"].(string); ok {
		d.Reason = r
	}
	return d
}

// shaPrefix returns the 8-char prefix per spec §3.7 R7. Card-cap on
// the OTel attribute; full SHA stays in the audit row (T5).
func shaPrefix(sha string) string {
	if len(sha) >= 8 {
		return sha[:8]
	}
	return sha
}

// bundleSHA computes the canonical SHA over sorted (path, body) pairs.
// T2 owns the production canonicalizer; this duplicate is intentional
// — T1 cannot import T2's policies/ until T2 lands (file-disjoint).
func bundleSHA(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(files[k]))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}


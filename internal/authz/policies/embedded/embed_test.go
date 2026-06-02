package embedded_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/trilamsr/regatta/internal/authz/policies/embedded"
)

// Drift sentinel: any change to the *.rego files MUST update this constant.
// Spec R11 — silent default-bundle drift between binary versions is a
// security regression; the failing assertion forces an explicit review.
func TestDefaultBundleSHA256_Stable(t *testing.T) {
	t.Parallel()
	const want = defaultBundleSHA256Expected
	if got := embedded.DefaultBundleSHA256; got != want {
		t.Fatalf("DefaultBundleSHA256 drift: got %q want %q\n"+
			"if this change is intentional, update the constant in embed_test.go "+
			"(spec §5 R11 — bundle drift is intentional iff this test is updated)", got, want)
	}
}

// Spec §3.5 — the ONE built-in exception: Principal{Tenant=="default", ID!=""}
// gets approval.decide allowed. Single-tenant zero-config deployments rely on it.
func TestDefaultBundle_HMACReviewer_AllowsApprovalDecide(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	q := prepareDecision(t, ctx, "data.regatta.v1.approval.decide.decision")

	in := map[string]any{
		"principal": map[string]any{
			"id":     "alice",
			"tenant": "default",
			"roles":  []string{},
		},
		"action":   "approval.decide",
		"resource": "01HZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	rs, err := q.Eval(ctx, rego.EvalInput(in))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(rs) != 1 {
		t.Fatalf("rs len = %d want 1; rs=%v", len(rs), rs)
	}
	dec, ok := rs[0].Expressions[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("decision not a map: %T %v", rs[0].Expressions[0].Value, rs[0].Expressions[0].Value)
	}
	if allow, _ := dec["allow"].(bool); !allow {
		t.Fatalf("hmac-reviewer principal denied: %v", dec)
	}
	if reason, _ := dec["reason"].(string); reason != "hmac-reviewer" {
		t.Fatalf("reason = %q want hmac-reviewer", reason)
	}
}

// Empty Principal.ID + Tenant="default" MUST fall through to default-deny.
// Pins spec §3.5 — exception requires BOTH conditions (tenant + non-empty id).
func TestDefaultBundle_EmptyPrincipal_Denies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	q := prepareDecision(t, ctx, "data.regatta.v1.approval.decide.decision")

	in := map[string]any{
		"principal": map[string]any{
			"id":     "",
			"tenant": "default",
			"roles":  []string{},
		},
		"action":   "approval.decide",
		"resource": "01HZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	rs, err := q.Eval(ctx, rego.EvalInput(in))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	dec, _ := rs[0].Expressions[0].Value.(map[string]any)
	if allow, _ := dec["allow"].(bool); allow {
		t.Fatalf("empty-id principal allowed; want default-deny: %v", dec)
	}
	if reason, _ := dec["reason"].(string); reason != "default-deny" {
		t.Fatalf("reason = %q want default-deny", reason)
	}
}

// Non-default tenant MUST fall through to default-deny; only "default" tenant
// gets the built-in HMAC-reviewer exception per spec §3.5.
func TestDefaultBundle_NonDefaultTenant_Denies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	q := prepareDecision(t, ctx, "data.regatta.v1.approval.decide.decision")

	in := map[string]any{
		"principal": map[string]any{
			"id":     "alice",
			"tenant": "acme",
			"roles":  []string{"reviewer"},
		},
		"action":   "approval.decide",
		"resource": "01HZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	rs, err := q.Eval(ctx, rego.EvalInput(in))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	dec, _ := rs[0].Expressions[0].Value.(map[string]any)
	if allow, _ := dec["allow"].(bool); allow {
		t.Fatalf("non-default tenant allowed; want default-deny: %v", dec)
	}
}

// Run actions have no built-in exception; default-deny across the board
// pins the spec §3.5 layout (run.rego defines run.{view,cost.view} only).
func TestDefaultBundle_RunActions_AllDeny(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, q := range []string{
		"data.regatta.v1.run.view.decision",
		"data.regatta.v1.run.cost.view.decision",
	} {
		prep := prepareDecision(t, ctx, q)
		in := map[string]any{
			"principal": map[string]any{"id": "alice", "tenant": "default", "roles": []string{}},
		}
		rs, err := prep.Eval(ctx, rego.EvalInput(in))
		if err != nil {
			t.Fatalf("Eval(%s): %v", q, err)
		}
		dec, _ := rs[0].Expressions[0].Value.(map[string]any)
		if allow, _ := dec["allow"].(bool); allow {
			t.Fatalf("%s allowed under default bundle; want deny", q)
		}
	}
}

// Embedded FS scope guard: tenant bundles MUST NEVER appear on disk in the
// embed. Spec §3.5 — tenant bundles live in substrate events, not the binary.
func TestDefaultBundle_EmbedScopeIsDefaultOnly(t *testing.T) {
	t.Parallel()
	paths := walk(t, embedded.FS, ".")
	for _, p := range paths {
		if !strings.HasPrefix(p, "regatta/v1/default/") {
			t.Fatalf("embed.FS leak: %q outside regatta/v1/default/", p)
		}
	}
	// Sanity floor: at least approval.rego + run.rego + data.json present.
	if len(paths) < 3 {
		t.Fatalf("embed.FS too small: %d entries; expected ≥ 3", len(paths))
	}
}

func loadModules(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	files := walk(t, embedded.FS, ".")
	for _, p := range files {
		if !strings.HasSuffix(p, ".rego") {
			continue
		}
		b, err := fs.ReadFile(embedded.FS, p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		out[p] = string(b)
	}
	return out
}

// prepareDecision compiles the embed.FS modules and prepares a query.
// Helper closes the slice-splat ergonomic gap in rego.New's variadic signature.
func prepareDecision(t *testing.T, ctx context.Context, query string) rego.PreparedEvalQuery {
	t.Helper()
	mods := loadModules(t)
	opts := make([]func(*rego.Rego), 0, 1+len(mods))
	opts = append(opts, rego.Query(query))
	for name, src := range mods {
		opts = append(opts, rego.Module(name, src))
	}
	prep, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("PrepareForEval(%s): %v", query, err)
	}
	return prep
}

func walk(t *testing.T, fsys fs.FS, root string) []string {
	t.Helper()
	var out []string
	if err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, path)
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

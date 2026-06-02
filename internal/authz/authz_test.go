package authz_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/web"
)

func TestAuthorizerCheck_DefaultBundle_AllowsHMACReviewer(t *testing.T) {
	t.Parallel()
	a := newTestAuthorizer(t)
	d, err := a.Check(context.Background(),
		web.Principal{ID: "alice", Tenant: "default"},
		authz.ActionApprovalDecide,
		"01HZX0000000000000000RESRC",
	)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !d.Allow {
		t.Fatalf("Allow=false; want true (default bundle hmac-reviewer rule). Decision=%+v", d)
	}
	if d.Reason != "hmac-reviewer" {
		t.Fatalf("Reason=%q; want %q", d.Reason, "hmac-reviewer")
	}
	if len(d.PolicyRevision) != 8 {
		t.Fatalf("PolicyRevision=%q; want 8-char SHA prefix per spec §3.7 R7", d.PolicyRevision)
	}
}

func TestAuthorizerCheck_UnknownTenant_ReturnsErrTenantUnknown(t *testing.T) {
	t.Parallel()
	a := newTestAuthorizer(t)
	_, err := a.Check(context.Background(),
		web.Principal{ID: "alice", Tenant: "acme-not-loaded"},
		authz.ActionApprovalDecide,
		"01HZX0000000000000000RESRC",
	)
	if !errors.Is(err, authz.ErrTenantUnknown) {
		t.Fatalf("err=%v; want errors.Is(err, ErrTenantUnknown)", err)
	}
}

func TestAuthorizerCheck_EmptyPrincipal_DefaultDenies(t *testing.T) {
	t.Parallel()
	a := newTestAuthorizer(t)
	d, err := a.Check(context.Background(),
		web.Principal{ID: "", Tenant: "default"},
		authz.ActionApprovalDecide,
		"01HZX0000000000000000RESRC",
	)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if d.Allow {
		t.Fatalf("Allow=true; want false (fail-closed for empty principal). Decision=%+v", d)
	}
	if d.Reason != "default-deny" {
		t.Fatalf("Reason=%q; want %q (default-deny)", d.Reason, "default-deny")
	}
}

func TestOpaStore_SwapIsAtomic(t *testing.T) {
	t.Parallel()
	a := newTestAuthorizer(t)
	ctx := context.Background()

	// One Check goroutine + one Reload goroutine. Reload installs N
	// distinct revisions; every Check Decision.PolicyRevision MUST
	// belong to {rev_0, rev_1, ..., rev_99} — never a torn mix.
	var seen sync.Map
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			d, err := a.Check(ctx,
				web.Principal{ID: "alice", Tenant: "default"},
				authz.ActionApprovalDecide,
				"01HZX0000000000000000RESRC",
			)
			if err != nil {
				t.Errorf("Check[%d]: %v", i, err)
				return
			}
			seen.Store(d.PolicyRevision, struct{}{})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := a.ReloadTenantForTest("default", i); err != nil {
				t.Errorf("Reload[%d]: %v", i, err)
				return
			}
		}
	}()

	wg.Wait()

	// Assert every observed PolicyRevision is a known revision (no torn read).
	known := a.KnownRevisionsForTest("default")
	seen.Range(func(k, _ any) bool {
		rev, _ := k.(string)
		if _, ok := known[rev]; !ok {
			t.Errorf("unknown revision %q observed; known set=%v", rev, known)
		}
		return true
	})
}

// newTestAuthorizer builds an authorizer hydrated with the inline default Rego module.
func newTestAuthorizer(t *testing.T) *authz.OPAAuthorizer {
	t.Helper()
	a, err := authz.NewOPAAuthorizer(authz.Config{})
	if err != nil {
		t.Fatalf("NewOPAAuthorizer: %v", err)
	}
	if err := a.Hydrate(context.Background()); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	return a
}

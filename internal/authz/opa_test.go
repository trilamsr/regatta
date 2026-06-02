package authz_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/web"
)

func TestOpaStore_ReloadDuringEval_NoTorn(t *testing.T) {
	t.Parallel()
	a := newTestAuthorizer(t)
	ctx := context.Background()

	const (
		goroutines  = 8
		evalsPerG   = 1000
		reloadCount = 50
	)

	var bad atomic.Int32
	var wg sync.WaitGroup
	wg.Add(goroutines + 1)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < evalsPerG; i++ {
				d, err := a.Check(ctx,
					web.Principal{ID: "alice", Tenant: "default"},
					authz.ActionApprovalDecide,
					"01HZX0000000000000000RESRC",
				)
				if err != nil {
					bad.Add(1)
					return
				}
				if len(d.PolicyRevision) != 8 {
					bad.Add(1)
					return
				}
			}
		}()
	}

	go func() {
		defer wg.Done()
		for i := 0; i < reloadCount; i++ {
			if err := a.ReloadTenantForTest("default", i); err != nil {
				bad.Add(1)
				return
			}
		}
	}()

	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("torn read or reload error count=%d", bad.Load())
	}
}

func TestAuthorizerCheck_CtxBoundPrincipal_Roundtrip(t *testing.T) {
	t.Parallel()
	p := web.Principal{ID: "alice", Tenant: "default", Roles: []string{"reviewer"}}
	ctx := authz.WithPrincipal(context.Background(), p)
	got, ok := authz.PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("PrincipalFromContext ok=false; want true")
	}
	if got.ID != p.ID || got.Tenant != p.Tenant {
		t.Fatalf("got=%+v; want=%+v", got, p)
	}
	// Missing ctx returns (zero, false).
	_, ok = authz.PrincipalFromContext(context.Background())
	if ok {
		t.Fatal("PrincipalFromContext on empty ctx ok=true; want false")
	}
}

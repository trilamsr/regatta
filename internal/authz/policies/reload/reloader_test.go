package reload_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/authz/policies/disk"
	"github.com/trilamsr/regatta/internal/authz/policies/embedded"
	"github.com/trilamsr/regatta/internal/authz/policies/reload"
)

// fsnotify event on a `.rego` write triggers Reload; CurrentRevision advances.
func TestReloader_FsnotifyTriggersReload(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DisableSighup: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	revBefore := az.CurrentRevision(authz.DefaultTenant)
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"reloaded\"}\n")

	waitForRevisionChange(t, az, revBefore, 2*time.Second)
}

// SIGHUP triggers an immediate reload (no debounce). Serial: signals are pid-wide.
func TestReloader_SighupTriggersReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGHUP unavailable on windows")
	}
	dir, az := setupAuthz(t)
	r := &reload.Reloader{
		Authorizer:      az,
		Loader:          &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:          authz.DefaultTenant,
		Debounce:        250 * time.Millisecond,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DisableFsnotify: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": false, \"reason\": \"sighup-loaded\"}\n")
	revBefore := az.CurrentRevision(authz.DefaultTenant)
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"sighup-v2\"}\n")

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill SIGHUP: %v", err)
	}
	waitForRevisionChange(t, az, revBefore, 2*time.Second)
}

// Half-written / syntactically broken `.rego` MUST NOT swap; retain-last-good.
func TestReloader_HalfWrittenFileRetainsLastGood(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DisableSighup: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	revBefore := az.CurrentRevision(authz.DefaultTenant)
	writeRego(t, dir, "approval.rego", "this is NOT valid rego\n")

	time.Sleep(500 * time.Millisecond)
	if got := az.CurrentRevision(authz.DefaultTenant); got != revBefore {
		t.Fatalf("revision advanced past broken bundle: was %q now %q", revBefore, got)
	}
}

// Editor backup files (.swp, ~, .tmp, hidden) must NOT fire a reload.
func TestReloader_IgnoresEditorBackupFiles(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	var reloadCount atomic.Int64
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		OnReload:   func(reload.Result) { reloadCount.Add(1) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	for _, drop := range []string{"approval.rego.swp", "approval.rego~", ".approval.rego.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, "regatta", "v1", "default", drop), []byte("garbage"), 0o644); err != nil {
			t.Fatalf("write %s: %v", drop, err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if got := reloadCount.Load(); got != 0 {
		t.Fatalf("expected 0 reloads from backup files, got %d", got)
	}
}

// CurrentRevision short-circuits a no-op reload when bundle SHA unchanged.
func TestReloader_NoOpReloadOnSameSHA(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	var reloadCount atomic.Int64
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		OnReload: func(res reload.Result) {
			if !res.Skipped {
				reloadCount.Add(1)
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	revBefore := az.CurrentRevision(authz.DefaultTenant)
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"v1\"}\n")
	waitForRevisionChange(t, az, revBefore, 2*time.Second)
	gotReloads := reloadCount.Load()

	// Same bytes again — fsnotify still fires but SHA short-circuit elides the swap.
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"v1\"}\n")
	time.Sleep(200 * time.Millisecond)
	if delta := reloadCount.Load() - gotReloads; delta != 0 {
		t.Fatalf("expected no-op on same SHA, got %d additional reloads", delta)
	}
}

// Concurrent SIGHUP + fsnotify storm keeps CurrentRevision non-empty (no torn read).
func TestReloader_ConcurrentSighupAndFsnotifyStoreConsistent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGHUP unavailable on windows")
	}
	dir, az := setupAuthz(t)
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				body := fmt.Sprintf(
					"package regatta.v1.approval.view\n\ndefault decision := {\"allow\": false, \"reason\": \"w%d-%d\"}\n",
					i, n,
				)
				writeRego(t, dir, "approval.rego", body)
				n++
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// CurrentRevision must never go empty mid-storm (torn-read guard).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if rev := az.CurrentRevision(authz.DefaultTenant); rev == "" {
				t.Errorf("torn read: empty revision mid-storm")
				return
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ctx cancel cleanly shuts down the watcher goroutine.
func TestReloader_RunReturnsOnContextCancel(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return after ctx cancel")
	}
}

// setupAuthz hydrates an OPAAuthorizer against a fresh policy_dir + embed fallback.
func setupAuthz(t *testing.T) (string, *authz.OPAAuthorizer) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "regatta", "v1", "default"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	loader := &disk.Loader{Dir: dir, Fallback: embeddedFallback()}
	az, err := authz.NewOPAAuthorizer(authz.Config{Loader: loader})
	if err != nil {
		t.Fatalf("NewOPAAuthorizer: %v", err)
	}
	if err := az.Hydrate(context.Background()); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	return dir, az
}

func writeRego(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, "regatta", "v1", "default", name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func startReloader(t *testing.T, r *reload.Reloader, ctx context.Context) {
	t.Helper()
	started := make(chan struct{})
	r.OnStart = func() { close(started) }
	go func() {
		if err := r.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("reloader exited: %v", err)
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("reloader did not start")
	}
}

func waitForRevisionChange(t *testing.T, az *authz.OPAAuthorizer, before string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if az.CurrentRevision(authz.DefaultTenant) != before {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("revision unchanged after %s (was %q)", timeout, before)
}

func embeddedFallback() authz.BundleLoader { return &embedFallback{} }

type embedFallback struct{}

func (e *embedFallback) Tenants(_ context.Context) ([]string, error) {
	return []string{authz.DefaultTenant}, nil
}

func (e *embedFallback) ActiveBundle(_ context.Context, tenant string) (string, map[string]string, error) {
	if tenant != authz.DefaultTenant {
		return "", nil, authz.ErrPolicyMissing
	}
	files, err := embedded.Files()
	if err != nil {
		return "", nil, err
	}
	return embedded.DefaultBundleSHA256, files, nil
}

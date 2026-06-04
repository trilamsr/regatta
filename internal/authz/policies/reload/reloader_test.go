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
	"github.com/trilamsr/regatta/internal/testutil"
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
	// Buffer holds at least one swap + one skip so OnReload never
	// back-pressures doReload.
	results := make(chan reload.Result, 16)
	r := &reload.Reloader{
		Authorizer: az,
		Loader:     &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:     authz.DefaultTenant,
		Debounce:   25 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		OnReload:   func(res reload.Result) { results <- res },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"v1\"}\n")
	// Observe the swap via OnReload — CurrentRevision flips before the
	// callback fires, so polling the revision lets the second write race
	// the snapshot. The callback is the only after-swap edge the test owns.
	first := waitForReloadResult(t, results, 2*time.Second)
	if first.Skipped || first.Err != nil {
		t.Fatalf("expected first reload to swap cleanly, got skipped=%v err=%v", first.Skipped, first.Err)
	}

	// Same bytes again — fsnotify still fires but SHA short-circuit elides the swap.
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"v1\"}\n")
	second := waitForReloadResult(t, results, 2*time.Second)
	if !second.Skipped {
		t.Fatalf("expected no-op (skipped=true) on same SHA, got skipped=%v err=%v rev=%q",
			second.Skipped, second.Err, second.RevisionAfter)
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
				time.Sleep(5 * time.Millisecond) // allow-sleep: producer rate-limit, not polling for a condition
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
			time.Sleep(10 * time.Millisecond) // allow-sleep: SIGHUP storm pacer, not polling for a condition
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

// TestReloader_PolicyDirRemovedThenRestored_ResumesWatching asserts F-HR8 watcher recovery (#365).
func TestReloader_PolicyDirRemovedThenRestored_ResumesWatching(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	r := &reload.Reloader{
		Authorizer:    az,
		Loader:        &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:        authz.DefaultTenant,
		Debounce:      25 * time.Millisecond,
		RetryInterval: 100 * time.Millisecond,
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DisableSighup: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	policyDir := filepath.Join(dir, "regatta", "v1", authz.DefaultTenant)
	revBefore := az.CurrentRevision(authz.DefaultTenant)
	if err := os.RemoveAll(policyDir); err != nil {
		t.Fatalf("rm -rf policy_dir: %v", err)
	}
	// Span ≥1 retry tick against the missing dir before recreate.
	time.Sleep(300 * time.Millisecond)
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("recreate policy_dir: %v", err)
	}
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"hr8-restored\"}\n")

	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 20*time.Millisecond, func() bool {
		return az.CurrentRevision(authz.DefaultTenant) != revBefore
	}, "revision unchanged after dir restore")
}

// TestReloader_RapidRmMkdirWriteCycleReloadsWithinTickBoundary asserts #702 R1 — rapid rm+mkdir+write inside one retry interval still reloads within the interval, not 2× it.
func TestReloader_RapidRmMkdirWriteCycleReloadsWithinTickBoundary(t *testing.T) {
	t.Parallel()
	dir, az := setupAuthz(t)
	const retry = 500 * time.Millisecond
	r := &reload.Reloader{
		Authorizer:    az,
		Loader:        &disk.Loader{Dir: dir, Fallback: embeddedFallback()},
		Tenant:        authz.DefaultTenant,
		Debounce:      25 * time.Millisecond,
		RetryInterval: retry,
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DisableSighup: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startReloader(t, r, ctx)

	policyDir := filepath.Join(dir, "regatta", "v1", authz.DefaultTenant)
	revBefore := az.CurrentRevision(authz.DefaultTenant)

	// Land rm+mkdir+write inside one retry window. Pre-fix: the Remove event
	// clears rootWatched but does not reset the ticker, so the next probe
	// fires up to `retry` after the last unrelated tick — observed stall is
	// up to 2× retry. Post-fix: Remove resets the ticker, so the next probe
	// fires `retry` after rm, bounding the stall to ≤ retry+debounce.
	if err := os.RemoveAll(policyDir); err != nil {
		t.Fatalf("rm -rf policy_dir: %v", err)
	}
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("recreate policy_dir: %v", err)
	}
	writeRego(t, dir, "approval.rego", "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"rapid-cycle\"}\n")

	waitCtx, waitCancel := context.WithTimeout(ctx, retry+500*time.Millisecond)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 20*time.Millisecond, func() bool {
		return az.CurrentRevision(authz.DefaultTenant) != revBefore
	}, "rapid rm+mkdir+write stalled past one retry interval (#702 R1)")
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	testutil.Eventually(t, ctx, 20*time.Millisecond, func() bool {
		return az.CurrentRevision(authz.DefaultTenant) != before
	}, fmt.Sprintf("revision unchanged (was %q)", before))
}

func waitForReloadResult(t *testing.T, ch <-chan reload.Result, timeout time.Duration) reload.Result {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(timeout):
		t.Fatalf("no reload result after %s", timeout)
		return reload.Result{}
	}
}

func embeddedFallback() authz.BundleLoader { return embedded.NewLoader() }

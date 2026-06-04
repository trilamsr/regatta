// Package reload triggers OPAAuthorizer.Reload from disk policy edits.
// Two trigger surfaces, one swap mechanism: SIGHUP (unix-only, no debounce)
// and fsnotify directory watching (cross-platform, 250 ms debounce). Both
// route through doReload, which uses Authorizer.CurrentRevision for a SHA
// short-circuit and treats a load/compile error as retain-last-good.
//
// Slim single-tenant hot-reload spec: §3.3.2 of
// docs/engineer/specs/2026-06-02-s3-t1-w8-opa-slim.md.
package reload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/authz/policies/disk"
)

// DefaultDebounce is the fsnotify event-coalesce window. SIGHUP bypasses
// the debounce — operator intent is explicit. Vim atomic-rename saves
// emit Create+Rename+Remove storms; 250 ms collapses them safely.
const DefaultDebounce = 250 * time.Millisecond

// DefaultRetryInterval is the watchLoop tick that retries Add on a
// missing/recreated policy_dir. 5 s matches operator-perceptible recovery
// latency after `rm -rf` + `mkdir`; tests inject a smaller value via
// Reloader.RetryInterval to avoid wall-clock load.
const DefaultRetryInterval = 5 * time.Second

// Trigger labels propagate to OTel `regatta.authz.reload.trigger`.
const (
	TriggerSighup   = "sighup"
	TriggerFsnotify = "fsnotify"
	TriggerBoot     = "boot"
)

// Reloader drives OPAAuthorizer.Reload from disk + signal events.
type Reloader struct {
	// Authorizer is the live store target. Reload mutates it atomically.
	Authorizer *authz.OPAAuthorizer

	// Loader produces the candidate bundle. Disk.Loader is the canonical
	// production wiring; tests may inject any BundleLoader-compatible
	// disk.Loader pointer.
	Loader *disk.Loader

	// Tenant is the slot to reload. Slim variant always passes
	// authz.DefaultTenant; kept as a field for Phase X.
	Tenant string

	// Debounce coalesces a storm of fsnotify events into a single reload.
	// Zero ⇒ DefaultDebounce.
	Debounce time.Duration

	// RetryInterval is how often watchLoop re-attempts Add on a missing
	// policy_dir (HR8 restore-after-removal). Zero ⇒ DefaultRetryInterval.
	// Production leaves this zero; tests shrink it to span fewer ticks.
	RetryInterval time.Duration

	// Logger receives structured load / error events. Required; nil
	// causes Run to return an error.
	Logger *slog.Logger

	// Tracer emits per-reload spans. Nil ⇒ otel.Tracer("internal/authz/reload").
	Tracer trace.Tracer

	// DisableSighup opts out of the SIGHUP handler. Default-on by zero
	// value (operator sets true for sandboxed/CI deployments). Spec
	// §3.5 safety.authz.reload_sighup mirrors with inverted polarity.
	DisableSighup bool

	// DisableFsnotify opts out of the watcher. Default-on by zero value.
	// Operator sets true on filesystems where inotify/kqueue is unstable.
	DisableFsnotify bool

	// OnStart fires after the watcher + signal goroutines are running.
	// Test hook (production wiring leaves nil).
	OnStart func()

	// OnReload fires after every doReload call, including no-op skips.
	// Test hook for assertions (production wiring leaves nil).
	OnReload func(Result)

	// Clock is the wall-clock source for the per-reload Duration
	// stamp. Nil falls back to time.Now. Same shape as the rest of
	// the regatta clock seam so tests share one fake clock across
	// reloader + downstream consumers.
	Clock func() time.Time
}

// Result is the per-reload report emitted to OnReload + Logger.
type Result struct {
	Trigger        string
	RevisionBefore string
	RevisionAfter  string
	Duration       time.Duration
	Skipped        bool  // true when SHA short-circuit hit
	Err            error // nil on success or skip; non-nil ⇒ retain-last-good
}

// Run blocks until ctx is canceled. Spawns watcher + signal goroutines;
// every event funnels into doReload via a single serialized goroutine so
// concurrent SIGHUP + fsnotify storms still produce one reload at a time
// (the atomic.Pointer CAS underneath would handle concurrent callers,
// but serializing here keeps the OTel spans + log lines readable).
func (r *Reloader) Run(ctx context.Context) error {
	if r.Authorizer == nil || r.Loader == nil {
		return errors.New("reload: Authorizer and Loader required")
	}
	if r.Logger == nil {
		return errors.New("reload: Logger required")
	}
	if r.Tenant == "" {
		r.Tenant = authz.DefaultTenant
	}
	if r.Debounce <= 0 {
		r.Debounce = DefaultDebounce
	}
	if r.RetryInterval <= 0 {
		r.RetryInterval = DefaultRetryInterval
	}
	if r.Tracer == nil {
		r.Tracer = otel.Tracer("internal/authz/reload")
	}

	// Reload requests funnel through one channel so doReload is never
	// concurrent with itself; the atomic.Pointer CAS underneath would
	// linearize anyway, but serializing here keeps spans + logs tidy.
	reloads := make(chan string, 8)

	var wg sync.WaitGroup
	watcherReady := make(chan struct{})
	signalReady := make(chan struct{})
	if !r.DisableFsnotify {
		wg.Add(1)
		go func() { defer wg.Done(); r.watchLoop(ctx, reloads, watcherReady) }()
	} else {
		close(watcherReady)
	}
	if !r.DisableSighup {
		wg.Add(1)
		go func() { defer wg.Done(); r.signalLoop(ctx, reloads, signalReady) }()
	} else {
		close(signalReady)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case trigger := <-reloads:
				r.doReload(ctx, trigger)
			}
		}
	}()

	// Block until BOTH triggers are live before OnStart fires. Without the
	// signal-ready gate a test-issued SIGHUP can race past an unregistered
	// signal.Notify and hit Go's default disposition — terminate the
	// process (`signal: hangup`). watcher-ready closes the matching window
	// for fsnotify writes landing pre-Add.
	for _, ready := range []chan struct{}{watcherReady, signalReady} {
		select {
		case <-ready:
		case <-ctx.Done():
			wg.Wait()
			return nil
		}
	}
	if r.OnStart != nil {
		r.OnStart()
	}
	<-ctx.Done()
	wg.Wait()
	return nil
}

// watchLoop owns the fsnotify.Watcher lifecycle. Re-add of the root on
// Remove events (operator `rm -rf` then recreate) keeps the watcher
// usable across mistakes; bounded retry stops the goroutine from spinning
// forever if the operator never restores the dir.
func (r *Reloader) watchLoop(ctx context.Context, reloads chan<- string, ready chan<- struct{}) {
	root := filepath.Join(r.Loader.Dir, "regatta", "v1", r.Tenant)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.Logger.Error("reload: fsnotify init failed", "err", err)
		close(ready)
		return
	}
	defer func() { _ = watcher.Close() }()

	// rootWatched tracks whether the current root has a live watch. On
	// transitions from absent→present, the retry ticker enqueues a reload
	// so writes that landed during the no-watch gap are not stranded.
	rootWatched := r.addWatch(watcher, root) == nil
	if !rootWatched {
		// Dir missing at boot is normal (operator hasn't populated it
		// yet); embed.FS fallback handles serving. Log + keep trying
		// every 5 s so a later `mkdir` resumes hot-reload.
		r.Logger.Info("reload: watcher waiting for policy dir", "dir", root)
	}
	close(ready)

	// Debounce timer reused across events. Initialized stopped.
	var (
		mu          sync.Mutex
		pendingHit  bool
		debounceCh  = make(chan struct{}, 1)
		retryTicker = time.NewTicker(r.RetryInterval)
	)
	defer retryTicker.Stop()

	armDebounce := func() {
		mu.Lock()
		defer mu.Unlock()
		if pendingHit {
			return
		}
		pendingHit = true
		time.AfterFunc(r.Debounce, func() {
			mu.Lock()
			pendingHit = false
			mu.Unlock()
			select {
			case debounceCh <- struct{}{}:
			default:
			}
		})
	}

	probe := func() {
		err := r.addWatch(watcher, root)
		if err == nil && !rootWatched {
			rootWatched = true
			select {
			case reloads <- TriggerFsnotify:
			case <-ctx.Done():
			}
			return
		}
		if err != nil {
			rootWatched = false
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-debounceCh:
			select {
			case reloads <- TriggerFsnotify:
			case <-ctx.Done():
				return
			}
		case <-retryTicker.C:
			probe()
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Remove) && ev.Name == root {
				// Rapid rm+mkdir+write inside one retry window stalls reload
				// up to 2×RetryInterval if we wait for the next tick (#702).
				// Probe immediately — same-tick mkdir is caught now; a still-
				// absent dir falls through to the ticker. Root-Remove must be
				// handled BEFORE the shouldHandleEvent filter (which rejects
				// non-`.rego` paths).
				rootWatched = false
				probe()
				continue
			}
			if !shouldHandleEvent(ev) {
				continue
			}
			armDebounce()
		case werr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.Logger.Warn("reload: watcher error", "err", werr)
		}
	}
}

// addWatch recursively adds dir + every subdir to watcher. fsnotify is
// non-recursive on linux/darwin; the spec §3.7 atomic-rename pattern only
// fires correctly when the *containing directory* is watched, never an
// individual file (Add(file) loses the watch on the post-rename inode).
func (r *Reloader) addWatch(w *fsnotify.Watcher, dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("reload: %s is not a directory", dir)
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && path != dir {
			return filepath.SkipDir
		}
		// Add is idempotent in fsnotify — re-adding an already-watched
		// path is a cheap no-op.
		return w.Add(path)
	})
}

// shouldHandleEvent applies the spec HR2 filter — editor backup / hidden
// files trigger zero reloads.
func shouldHandleEvent(ev fsnotify.Event) bool {
	if ev.Op == 0 {
		return false
	}
	base := filepath.Base(ev.Name)
	if base == "" || strings.HasPrefix(base, ".") {
		return false
	}
	if strings.HasSuffix(base, ".swp") ||
		strings.HasSuffix(base, ".swo") ||
		strings.HasSuffix(base, ".swx") ||
		strings.HasSuffix(base, ".tmp") ||
		strings.HasSuffix(base, ".bak") ||
		strings.HasSuffix(base, "~") {
		return false
	}
	// Only react to `.rego` + `data.json` + directory operations (the
	// containing-dir watch needs to react to subdir create/remove too).
	if strings.HasSuffix(base, ".rego") || base == "data.json" {
		return true
	}
	// Bare directory events (Create on a new subdir) — let through so
	// the watcher loop can Add the new path on the next retry tick.
	return false
}

// doReload re-scans the loader and CASes the store. Errors are logged +
// recorded as an OTel span event; the prior bundle stays active.
func (r *Reloader) doReload(ctx context.Context, trigger string) {
	ctx, span := r.Tracer.Start(ctx, "authz.reload")
	defer span.End()
	clock := r.Clock
	if clock == nil {
		clock = time.Now
	}
	start := clock()

	revBefore := r.Authorizer.CurrentRevision(r.Tenant)
	sha, _, err := r.Loader.ActiveBundle(ctx, r.Tenant)
	res := Result{Trigger: trigger, RevisionBefore: revBefore, Duration: clock().Sub(start)}
	span.SetAttributes(
		attribute.String("regatta.authz.reload.trigger", trigger),
		attribute.String("regatta.authz.reload.revision_before", revBefore),
	)
	if err != nil {
		res.Err = err
		res.RevisionAfter = revBefore
		span.RecordError(err)
		span.AddEvent("regatta.authz.reload.failed")
		r.Logger.Error("reload: load failed (retain-last-good)", "trigger", trigger, "err", err)
		r.report(res)
		return
	}

	// SHA short-circuit: bundle bytes unchanged ⇒ no compile + no swap.
	if revPrefix(sha) == revBefore {
		res.Skipped = true
		res.RevisionAfter = revBefore
		res.Duration = clock().Sub(start)
		span.SetAttributes(
			attribute.String("regatta.authz.reload.revision_after", revBefore),
			attribute.Bool("regatta.authz.reload.skipped", true),
			attribute.Int64("regatta.authz.reload.duration_micros", res.Duration.Microseconds()),
		)
		r.Logger.Debug("reload: no-op (sha unchanged)", "trigger", trigger, "revision", revBefore)
		r.report(res)
		return
	}

	if err := r.Authorizer.Reload(ctx, r.Tenant); err != nil {
		res.Err = err
		res.RevisionAfter = revBefore
		span.RecordError(err)
		span.AddEvent("regatta.authz.reload.failed")
		r.Logger.Error("reload: compile+swap failed (retain-last-good)", "trigger", trigger, "err", err)
		r.report(res)
		return
	}

	revAfter := r.Authorizer.CurrentRevision(r.Tenant)
	res.RevisionAfter = revAfter
	res.Duration = clock().Sub(start)
	span.SetAttributes(
		attribute.String("regatta.authz.reload.revision_after", revAfter),
		attribute.Int64("regatta.authz.reload.duration_micros", res.Duration.Microseconds()),
	)
	r.Logger.Info("reload: bundle swapped",
		"trigger", trigger,
		"revision_before", revBefore,
		"revision_after", revAfter,
		"duration_micros", res.Duration.Microseconds(),
	)
	r.report(res)
}

func (r *Reloader) report(res Result) {
	if r.OnReload != nil {
		r.OnReload(res)
	}
}

// revPrefix mirrors authz.shaPrefix (unexported in that package) — keeps
// the no-op compare apples-to-apples against CurrentRevision's 8-char
// stored value.
func revPrefix(sha string) string {
	if len(sha) >= 8 {
		return sha[:8]
	}
	return sha
}

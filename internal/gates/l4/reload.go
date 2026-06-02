// Hot-reload of the L4 prompt template. Two trigger surfaces, one swap
// mechanism — pattern copy from internal/authz/policies/reload (W8 #367).
// Single-file variant: L4 has one template, no tenant indirection, no
// compile step beyond text/template.Parse, so the W8 BundleLoader +
// Authorizer interfaces don't fit and a thin local reloader is shorter
// than an adapter would be (feedback_research_design_principles —
// reuse the pattern, not the abstraction when it stretches).
package l4

import (
	"context"
	"errors"
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
)

// DefaultReloadDebounce mirrors W8 (250 ms collapses editor-storm).
const DefaultReloadDebounce = 250 * time.Millisecond

// Trigger labels propagate to OTel `regatta.gates.l4.reload.trigger`.
const (
	TriggerSighup   = "sighup"
	TriggerFsnotify = "fsnotify"
	TriggerBoot     = "boot"
)

// Reloader watches a disk-override prompt path and swaps the active
// template on SIGHUP or fsnotify events. Missing path, read error, or
// parse error retains the last-good slot — the embedded default always
// serves as the floor.
type Reloader struct {
	// Path is the disk-override file (typically <repo>/prompts/adversarial.tmpl).
	// Empty path is a config error at Run-time.
	Path string

	// Debounce coalesces fsnotify event storms. Zero ⇒ DefaultReloadDebounce.
	Debounce time.Duration

	// Logger receives structured load + error events. Required.
	Logger *slog.Logger

	// Tracer emits per-reload spans. Nil ⇒ otel.Tracer("internal/gates/l4").
	Tracer trace.Tracer

	// DisableSighup opts out of the SIGHUP handler (sandboxed/CI).
	DisableSighup bool

	// DisableFsnotify opts out of the watcher (unreliable FS).
	DisableFsnotify bool

	// OnStart fires after watcher + signal goroutines are live. Test hook.
	OnStart func()

	// OnReload fires after every doReload, including no-op skips. Test hook.
	OnReload func(Result)
}

// Result is the per-reload report.
type Result struct {
	Trigger        string
	RevisionBefore string
	RevisionAfter  string
	Duration       time.Duration
	Skipped        bool  // true on SHA short-circuit
	Err            error // nil ⇒ success or skip; non-nil ⇒ retain-last-good
}

// Run blocks until ctx is canceled. Mirrors W8 reload.Reloader.Run
// modulo the single-file watch + no Authorizer indirection.
func (r *Reloader) Run(ctx context.Context) error {
	if r.Path == "" {
		return errors.New("l4 reload: Path required")
	}
	if r.Logger == nil {
		return errors.New("l4 reload: Logger required")
	}
	if r.Debounce <= 0 {
		r.Debounce = DefaultReloadDebounce
	}
	if r.Tracer == nil {
		r.Tracer = otel.Tracer("internal/gates/l4")
	}

	reloads := make(chan string, 8)
	var wg sync.WaitGroup
	watcherReady := make(chan struct{})

	if !r.DisableFsnotify {
		wg.Add(1)
		go func() { defer wg.Done(); r.watchLoop(ctx, reloads, watcherReady) }()
	} else {
		close(watcherReady)
	}
	if !r.DisableSighup {
		wg.Add(1)
		go func() { defer wg.Done(); r.signalLoop(ctx, reloads) }()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Boot reload picks up any disk override that already exists.
		// Always emits one OnReload so wiring tests observe the initial read.
		r.doReload(ctx, TriggerBoot)
		for {
			select {
			case <-ctx.Done():
				return
			case trigger := <-reloads:
				r.doReload(ctx, trigger)
			}
		}
	}()

	select {
	case <-watcherReady:
	case <-ctx.Done():
		wg.Wait()
		return nil
	}
	if r.OnStart != nil {
		r.OnStart()
	}
	<-ctx.Done()
	wg.Wait()
	return nil
}

// watchLoop owns the fsnotify.Watcher. Watches the PARENT dir of Path
// because atomic-rename editors (vim default, neovim default) drop the
// inode after save; Add(file) loses the watch but Add(dir) survives.
// Mirrors W8 §3.7.
func (r *Reloader) watchLoop(ctx context.Context, reloads chan<- string, ready chan<- struct{}) {
	dir := filepath.Dir(r.Path)
	base := filepath.Base(r.Path)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.Logger.Error("l4 reload: fsnotify init failed", "err", err)
		close(ready)
		return
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(dir); err != nil {
		r.Logger.Info("l4 reload: watcher waiting for prompt dir", "dir", dir, "err", err)
	}
	close(ready)

	var (
		mu         sync.Mutex
		pending    bool
		debounceCh = make(chan struct{}, 1)
		retry      = time.NewTicker(5 * time.Second)
	)
	defer retry.Stop()

	arm := func() {
		mu.Lock()
		defer mu.Unlock()
		if pending {
			return
		}
		pending = true
		time.AfterFunc(r.Debounce, func() {
			mu.Lock()
			pending = false
			mu.Unlock()
			select {
			case debounceCh <- struct{}{}:
			default:
			}
		})
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
		case <-retry.C:
			_ = watcher.Add(dir)
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !shouldHandleEvent(ev, base) {
				continue
			}
			arm()
		case werr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.Logger.Warn("l4 reload: watcher error", "err", werr)
		}
	}
}

// shouldHandleEvent filters editor backup + sibling files. Mirrors W8
// HR2 filter but scoped to the single tracked file basename.
func shouldHandleEvent(ev fsnotify.Event, base string) bool {
	if ev.Op == 0 {
		return false
	}
	name := filepath.Base(ev.Name)
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".swp") ||
		strings.HasSuffix(name, ".swo") ||
		strings.HasSuffix(name, ".swx") ||
		strings.HasSuffix(name, ".tmp") ||
		strings.HasSuffix(name, ".bak") ||
		strings.HasSuffix(name, "~") {
		return false
	}
	// Only react to events on the tracked file. Vim atomic-rename hits
	// Create+Rename on `base`; debounce collapses the storm.
	return name == base
}

// doReload reads the file, parses, and CASes the active slot. Errors
// log + record an OTel event; the prior slot stays active.
func (r *Reloader) doReload(ctx context.Context, trigger string) {
	_, span := r.Tracer.Start(ctx, "l4.prompt.reload")
	defer span.End()
	start := time.Now()

	prev := active.Load()
	res := Result{Trigger: trigger, RevisionBefore: prev.sha}
	span.SetAttributes(
		attribute.String("regatta.gates.l4.reload.trigger", trigger),
		attribute.String("regatta.gates.l4.reload.revision_before", prev.sha),
	)

	body, err := os.ReadFile(r.Path)
	if err != nil {
		res.Err = err
		res.RevisionAfter = prev.sha
		res.Duration = time.Since(start)
		span.RecordError(err)
		span.AddEvent("regatta.gates.l4.reload.failed")
		r.Logger.Error("l4 reload: read failed (retain-last-good)", "trigger", trigger, "err", err)
		r.report(res)
		return
	}

	next, err := parseSlot(string(body))
	if err != nil {
		res.Err = err
		res.RevisionAfter = prev.sha
		res.Duration = time.Since(start)
		span.RecordError(err)
		span.AddEvent("regatta.gates.l4.reload.failed")
		r.Logger.Error("l4 reload: parse failed (retain-last-good)", "trigger", trigger, "err", err)
		r.report(res)
		return
	}

	// SHA short-circuit: identical bytes ⇒ no swap.
	if next.sha == prev.sha {
		res.Skipped = true
		res.RevisionAfter = prev.sha
		res.Duration = time.Since(start)
		span.SetAttributes(
			attribute.String("regatta.gates.l4.reload.revision_after", prev.sha),
			attribute.Bool("regatta.gates.l4.reload.skipped", true),
			attribute.Int64("regatta.gates.l4.reload.duration_micros", res.Duration.Microseconds()),
		)
		r.Logger.Debug("l4 reload: no-op (sha unchanged)", "trigger", trigger, "revision", prev.sha)
		r.report(res)
		return
	}

	active.Store(next)
	res.RevisionAfter = next.sha
	res.Duration = time.Since(start)
	span.SetAttributes(
		attribute.String("regatta.gates.l4.reload.revision_after", next.sha),
		attribute.Int64("regatta.gates.l4.reload.duration_micros", res.Duration.Microseconds()),
	)
	r.Logger.Info("l4 reload: prompt swapped",
		"trigger", trigger,
		"revision_before", prev.sha,
		"revision_after", next.sha,
		"duration_micros", res.Duration.Microseconds(),
	)
	r.report(res)
}

func (r *Reloader) report(res Result) {
	if r.OnReload != nil {
		r.OnReload(res)
	}
}

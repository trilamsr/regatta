package secrets

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// snapshot is the read-mostly cache payload. Readers acquire the
// pointer via atomic.Pointer.Load — no lock, no allocation, no
// branch beyond the nil check. Rotation publishes a fresh snapshot
// via Store; readers see fully-old or fully-new, never partial.
type snapshot struct {
	values map[string]Value  // canonical key → Value (zero Value iff missing)
	source map[string]string // canonical key → adapter name ("env", sourceMissing, …)
	at     time.Time
}

// Cache is the boot-loaded secret store. Hot path: Get → one
// atomic.Pointer.Load + one map read. Rotation path: SIGHUP wakes a
// normal goroutine that re-fetches the chain and atomically publishes
// the new snapshot. No raw signal-handler work — `signal.Notify`
// buffers signals onto a Go channel per `os/signal` docs.
type Cache struct {
	ptr atomic.Pointer[snapshot]
}

// NewCache constructs an empty cache. Call Load to populate before
// readers run.
func NewCache() *Cache {
	c := &Cache{}
	c.ptr.Store(emptySnapshot())
	return c
}

// emptySnapshot is the zero-value baseline so readers never see a nil
// snapshot — simpler hot-path code.
func emptySnapshot() *snapshot {
	return &snapshot{
		values: map[string]Value{},
		source: map[string]string{},
		at:     time.Now(),
	}
}

// Load fetches every canonical key through the chain and publishes
// the new snapshot atomically. Missing keys land as zero Value with
// source=sourceMissing — consumers see (Value{}, sourceMissing, false) and
// degrade per their own policy.
func (c *Cache) Load(ctx context.Context, fetcher Fetcher, logger *slog.Logger) {
	snap := fetchAll(ctx, fetcher, logger)
	c.ptr.Store(snap)
}

// Get reads a cached value. The bool reports presence — operators
// see (Value{}, sourceMissing, false) for unfetched keys.
func (c *Cache) Get(key string) (Value, string, bool) {
	snap := c.ptr.Load()
	if snap == nil {
		return Value{}, sourceMissing, false
	}
	v, ok := snap.values[key]
	src := snap.source[key]
	if !ok || src == sourceMissing {
		return Value{}, sourceMissing, false
	}
	return v, src, true
}

// Snapshot returns a (read-only) view of source labels for diagnostics.
// Values are NEVER returned by this method — only the source label
// per key, so `regatta secret status` cannot accidentally leak.
func (c *Cache) Snapshot() map[string]string {
	snap := c.ptr.Load()
	if snap == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(snap.source))
	for k, v := range snap.source {
		out[k] = v
	}
	return out
}

// OnRotate is the optional callback fired after a successful SIGHUP
// re-fetch. callers wire it to record a substrate audit row so
// `regatta events tail` carries a durable rotation trail beyond the
// slog INFO line. Nil means no callback.
type OnRotate func(ctx context.Context, chain string, keys int)

// Run blocks until ctx is cancelled, watching for SIGHUP. Each SIGHUP
// triggers a re-fetch + atomic publish. The signal channel is buffered
// (size 1) so a burst of HUPs collapses into one refetch — exactly
// the operator intent ("reload now"). onRotate, if non-nil, fires
// after the snapshot publish so the caller can append a substrate
// audit event.
func (c *Cache) Run(ctx context.Context, fetcher Fetcher, logger *slog.Logger, onRotate OnRotate) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	defer signal.Stop(sigCh)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			snap := fetchAll(ctx, fetcher, logger)
			c.ptr.Store(snap)
			if logger != nil {
				logger.LogAttrs(ctx, slog.LevelInfo, "secrets_rotated",
					slog.String("chain", fetcher.Name()),
					slog.Int("keys", len(snap.values)),
				)
			}
			if onRotate != nil {
				onRotate(ctx, fetcher.Name(), len(snap.values))
			}
		}
	}
}

// Rotate forces a re-fetch + atomic publish without sending SIGHUP.
// Used by `regatta secret reload` (the thin wrapper) and by tests
// that need to drive rotation deterministically.
func (c *Cache) Rotate(ctx context.Context, fetcher Fetcher, logger *slog.Logger) {
	c.ptr.Store(fetchAll(ctx, fetcher, logger))
}

// fetchAll walks the canonical key list and returns a fresh snapshot.
// One INFO summary line ("secrets_resolved resolved=N missing=M") per
// boot keeps the journal readable when docker restart-on-failure cycles
// the daemon; per-key detail demoted to DEBUG. Missing-mandatory keys
// stay at WARN so operator misconfig still surfaces above INFO.
func fetchAll(ctx context.Context, fetcher Fetcher, logger *slog.Logger) *snapshot {
	snap := &snapshot{
		values: make(map[string]Value, len(CanonicalKeys)),
		source: make(map[string]string, len(CanonicalKeys)),
		at:     time.Now(),
	}
	var resolved, missing int
	for _, key := range CanonicalKeys {
		v, src, err := GetWithSource(ctx, fetcher, key)
		if err != nil {
			snap.values[key] = Value{}
			snap.source[key] = sourceMissing
			missing++
			if logger != nil {
				level := slog.LevelDebug
				if !OptionalKeys[key] {
					level = slog.LevelWarn
				}
				logger.LogAttrs(ctx, level, "secret_missing",
					slog.String("key", key),
					slog.String("chain", fetcher.Name()),
				)
			}
			continue
		}
		snap.values[key] = v
		snap.source[key] = src
		resolved++
		if logger != nil {
			logger.LogAttrs(ctx, slog.LevelDebug, "secret_resolved",
				slog.String("key", key),
				slog.String("source", src),
				slog.Int("bytes", v.Len()),
			)
		}
	}
	if logger != nil {
		logger.LogAttrs(ctx, slog.LevelInfo, "secrets_resolved",
			slog.Int("resolved", resolved),
			slog.Int("missing", missing),
			slog.Int("total", len(CanonicalKeys)),
			slog.String("chain", fetcher.Name()),
		)
	}
	return snap
}

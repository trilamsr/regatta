package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// BriefLoader narrows program.BriefLoader to the seam PollOnce uses —
// interface-typed here to break the cycle (program imports orchestrator
// for ErrHMACInvalid / sentinels).
type BriefLoader interface {
	Sync(ctx context.Context, pollStartedAt time.Time) error
}

// ItemBodyLoader returns the raw markdown brief for workItemID so
// ScheduleOnce can populate spawner.Request.ItemBody before Spawn. Nil
// or a (",false") return drops back to the identifier-only prompt
// without blocking the spawn — a missing brief MUST NOT strand the
// reservation. Implementations sit in cmd/regatta/serve.go for the
// production wiring; tests stub with a closure over an in-memory map.
type ItemBodyLoader func(ctx context.Context, workItemID string) (body string, ok bool)

// Config holds tunables and dependencies for an Orchestrator, wired via
// construction-time DI so tests can swap any seam without touching
// internal helpers.
type Config struct {
	// AdapterSync mirrors the spec adapter into work_items (spec §2.9 step 3). Required.
	AdapterSync *adaptersync.Syncer

	// BriefLoader verifies + upserts brief children (spec §2.4). Required.
	BriefLoader BriefLoader

	// DB is the universal state store. Required.
	DB *state.DB

	// Scheduler reserves spawnable work_items into agents — lane caps, lock TTL, hotspot resolver live in scheduler's own Config. Required.
	Scheduler *scheduler.Scheduler

	// Spawner launches the reserved agents. Required.
	Spawner spawner.Spawner

	// ItemBody resolves the per-dispatch markdown brief. Nil = identifier-only prompt (regression-safe).
	ItemBody ItemBodyLoader

	// RepoRoot is the absolute repo path the worker subprocess targets; threaded into spawner.Request.RepoRoot. Empty in unit tests.
	RepoRoot string

	// DBPath is the on-disk sqlite path; the process-level lockfile derives as <dbPath>.lock.
	DBPath string

	// PollInterval is how often PollOnce runs. Default 30s.
	PollInterval time.Duration

	// TickInterval is how often the Scheduler ticks. Default 5s.
	TickInterval time.Duration

	// HeartbeatInterval refreshes locks held by non-terminal agents — default 60s (design.md §Concurrency & soft-lock policy).
	HeartbeatInterval time.Duration

	// LockTTL is the heartbeat lease used by ExpireStaleLocks. Default 15m.
	LockTTL time.Duration

	// Logger sinks orchestrator lifecycle events; nil falls back to slog.Default() so embedded callers still get output (spec §4.1).
	Logger *slog.Logger

	// Tracer opens spans; nil resolves to otel.Tracer("orchestrator") — noop until obs/otel.Setup runs.
	Tracer trace.Tracer

	// Meter is the OTel instrument factory; nil resolves lazily via obs.MeterScopeOrchestrator at the first ResolveMeter() so global MeterProvider Setup wins by default.
	Meter metric.Meter

	// Clock sources wall-clock for PollOnce + ScheduleOnce telemetry.
	// Nil falls back to time.Now. Crash-recovery paths (Recover,
	// Heartbeat) intentionally do NOT route through Clock — they reflect
	// real-world lock liveness and must keep using wall time so a
	// deterministic test clock cannot mask a stuck heartbeat.
	Clock func() time.Time
}

// ResolveMeter returns the configured meter or falls back lazily to the
// global provider's scoped meter so a provider swap takes effect on the
// next call.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeOrchestrator)
}

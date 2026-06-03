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

// BriefLoader narrows program.BriefLoader to the seam PollOnce
// uses. Defined here as an interface to break the import cycle:
// program imports orchestrator (for ErrHMACInvalid / sentinels),
// so orchestrator cannot import program in turn.
type BriefLoader interface {
	Sync(ctx context.Context, pollStartedAt time.Time) error
}

// Config holds tunables and dependencies for an Orchestrator.
// Construction-time DI: callers wire AdapterSync, BriefLoader, and
// Scheduler externally so tests can swap any seam without touching the
// orchestrator's internal helpers.
type Config struct {
	// AdapterSync mirrors the spec adapter into work_items per
	// spec §2.9 step 3. Required.
	AdapterSync *adaptersync.Syncer

	// BriefLoader verifies + upserts brief children into work_items
	// per spec §2.4. Required. Interface-typed (not concrete
	// program.BriefLoader) so this package does not transitively
	// import internal/program, which already imports us for
	// ErrHMACInvalid / ErrCascadeNonConverging.
	BriefLoader BriefLoader

	// DB is the universal state store. Required.
	DB *state.DB

	// Scheduler reserves spawnable work_items into agents. Required.
	// Lane caps, lock TTL, and hotspot resolver come from the
	// scheduler's own Config — Orchestrator no longer owns those.
	Scheduler *scheduler.Scheduler

	// Spawner launches the reserved agents. Required.
	Spawner spawner.Spawner

	// DBPath is the on-disk sqlite path. Used to derive the
	// process-level lockfile (<dbPath>.lock).
	DBPath string

	// PollInterval is how often PollOnce runs in the Run loop.
	// Default: 30s.
	PollInterval time.Duration

	// TickInterval is how often the Scheduler ticks. Default: 5s.
	TickInterval time.Duration

	// HeartbeatInterval is how often Heartbeat refreshes locks held
	// by non-terminal agents. Default: 60s, matching design.md
	// §Concurrency & soft-lock policy.
	HeartbeatInterval time.Duration

	// LockTTL is the heartbeat lease used by ExpireStaleLocks.
	// Default: 15 minutes.
	LockTTL time.Duration

	// Logger is the structured-event sink for orchestrator lifecycle
	// events. Nil falls back to slog.Default() so embedded callers
	// still get output without panicking (spec §4.1).
	Logger *slog.Logger

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("orchestrator") which resolves to
	// the global provider — noop until obs/otel.Setup runs. Mirrors
	// the Config.Logger DI normalization per W6 spec §3.3 +
	// feedback_spec_pattern_authority.
	Tracer trace.Tracer

	// Meter is the OTel instrument factory for orchestrator
	// lifecycle telemetry. Nil resolves to obs.MeterScopeOrchestrator
	// at the first ResolveMeter() call so the global MeterProvider
	// Setup wires (or a noop when Setup was skipped) wins by default.
	// Mirrors the W6 Config.Tracer pattern so callers stay on one DI
	// seam across trace + metric.
	Meter metric.Meter

	// Clock is the wall-clock source for PollOnce + ScheduleOnce
	// telemetry. Nil falls back to time.Now (production wiring). The
	// crash-recovery paths (Recover, Heartbeat) intentionally do NOT
	// route through Clock — they reflect real-world lock liveness and
	// must keep using wall time so a deterministic test clock cannot
	// mask a stuck heartbeat. Same shape as state.OpenWithClock,
	// spawner.Config.Clock so tests share one injection seam.
	Clock func() time.Time
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. The fallback is lazy so a global
// provider swap (e.g. test injection of a noop provider) takes effect
// on the next call. Matches the W6 Config.Tracer nil-fallback shape.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeOrchestrator)
}

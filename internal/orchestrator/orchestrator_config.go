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

// ItemBodyLoader feeds spawner.Request.ItemBody; nil or ok=false degrades to identifier-only prompt so a missing brief never strands the reservation.
type ItemBodyLoader func(ctx context.Context, workItemID string) (body string, ok bool)

// HeartbeatToucher is the narrow seam the orchestrator uses to refresh
// the /healthz heartbeat cell every Run-loop tick. The interface stays
// in this package so cmd/regatta can wire the live health.HeartbeatCell
// without forcing orchestrator to import the health package (#1218).
type HeartbeatToucher interface {
	Touch()
}

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

	// SpawnerBackend names the active spawner ("stub" | "claude"); empty
	// is treated as stub. Recover compares it against each recovered
	// agent's session_id prefix so a stub-written ghost is reaped — not
	// re-attached — when the daemon reboots under the claude backend
	// (MAY-79).
	SpawnerBackend string

	// ItemBody resolves the per-dispatch markdown brief. Nil = identifier-only prompt (regression-safe).
	ItemBody ItemBodyLoader

	// RepoRoot is the absolute repo path the worker subprocess targets; threaded into spawner.Request.RepoRoot. Empty in unit tests.
	RepoRoot string

	// DestructiveOpsDeny + AgentDestructiveOpsAllow mirror the resolved safety.* lists; threaded into spawner.Request so the brief renders the concrete deny/allow policy at dispatch (MAY-97, MAY-258). Empty ⇒ no policy section in the brief.
	DestructiveOpsDeny       []string
	AgentDestructiveOpsAllow []string

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

	// HealthHeartbeat is the /healthz liveness cell the orchestrator
	// Touches every Run-loop tick so the readiness probe reports a fresh
	// age. Nil leaves the cell stale — /healthz then reports
	// heartbeat.status=stale at the default 365-day age_seconds the
	// cell uses for never-touched (#1218 root cause: orphaned cell).
	HealthHeartbeat HeartbeatToucher
}

// ResolveMeter returns the configured meter or falls back lazily to the
// global provider's scoped meter so a provider swap takes effect on the
// next call.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeOrchestrator)
}

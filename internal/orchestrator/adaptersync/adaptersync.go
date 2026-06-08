// Package adaptersync mirrors the read-only SpecAdapter into
// state.work_items. Step 3 of orchestrator.PollOnce (spec §2.9).
// Tombstones rows the adapter no longer returns; see
// cascadeChildrenOfArchivedPrograms for parent→child fan-out.
package adaptersync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// SpecAdapter is the subset of schemas.SpecAdapter the Syncer needs:
// List drives the mirror; Capabilities supplies MinPollInterval so the
// Syncer can skip List inside the rate-budget window (#888).
type SpecAdapter interface {
	List(ctx context.Context) ([]schemas.WorkItem, error)
	Capabilities() schemas.Capabilities
}

// Config holds dependencies for a Syncer. Mirrors the Config.Logger
// DI pattern used by orchestrator, scheduler, and reaper so all
// components share one injection shape.
type Config struct {
	// Adapter mirrors the orchestrator's read surface. Required.
	Adapter SpecAdapter

	// DB is the universal state store. Required.
	DB *state.DB

	// Logger is the structured-event sink for adapter-skip
	// diagnostics. Nil falls back to slog.Default() so embedded
	// callers still get output without panicking (spec §4.1 + §5.8).
	Logger *slog.Logger

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("adaptersync") which resolves to
	// the global provider — noop until obs/otel.Setup runs. Per W6
	// spec §3.3 + feedback_spec_pattern_authority.
	Tracer trace.Tracer

	// Meter is the OTel instrument factory for adaptersync telemetry.
	// Nil resolves to obs.MeterScopeAdaptersync at the first
	// ResolveMeter() call so the global MeterProvider Setup wires (or
	// a noop when Setup was skipped) wins by default. Mirrors the W6
	// Config.Tracer pattern so callers stay on one DI seam across
	// trace + metric.
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. The fallback is lazy so a global
// provider swap (e.g. test injection of a noop provider) takes effect
// on the next call. Matches the W6 Config.Tracer nil-fallback shape.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeAdaptersync)
}

// Syncer pairs an adapter with the state DB. Timestamps are passed
// to Sync rather than held here, so concurrent producers sharing a
// DB cannot race on a shared clock.
//
// lastPoll holds the most recent Sync attempt timestamp (success OR
// error) so the MinPollInterval gate can short-circuit re-polls inside
// the rate-budget window — including the error case where retry-inside-
// throttle worsens the limit (#888). Single-adapter scope; concurrency
// is provided by orchestrator.PollOnce's flock.
type Syncer struct {
	adapter           SpecAdapter
	db                *state.DB
	log               *slog.Logger
	tracer            trace.Tracer
	adapterPollErrors metric.Int64Counter
	lastPoll          time.Time
}

// New constructs a Syncer from a Config. Returns an error if any
// required field (Adapter, DB) is nil — surfacing misconfiguration at
// boot instead of deferring a nil-deref panic to the first Sync call.
// nil Logger falls back to slog.Default() (spec §4.1).
func New(cfg Config) (*Syncer, error) {
	if cfg.Adapter == nil {
		return nil, fmt.Errorf("adaptersync: Config.Adapter is required")
	}
	if cfg.DB == nil {
		return nil, fmt.Errorf("adaptersync: Config.DB is required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("adaptersync")
	}
	meter := cfg.ResolveMeter()
	adapterPollErrors, err := meter.Int64Counter("regatta.adaptersync.adapter_poll.errors_total")
	if err != nil {
		adapterPollErrors, _ = obs.Meter(obs.MeterScopeAdaptersyncFallback).Int64Counter("regatta.adaptersync.adapter_poll.errors_total")
	}
	return &Syncer{adapter: cfg.Adapter, db: cfg.DB, log: log, tracer: tracer, adapterPollErrors: adapterPollErrors}, nil
}

// Sync upserts adapter items, tombstones rows the adapter no longer
// returns, then reconciles orphaned children. Idempotent on partial
// failure: a mid-cascade crash converges on the next tick.
//
// Empty adapter.List skips the tombstone sweep (transient upstream
// hiccups must not wipe the queue). Unmappable items are warn-logged
// through the injected sink and skipped — one bad row must not stop
// a poll. Per spec §3 any DB error returns immediately; mapped items
// land in one BatchUpsertWorkItems call (issue #89: was N round-trips).
func (s *Syncer) Sync(ctx context.Context, pollStartedAt time.Time) error {
	// W6 spec §8 T5: open a span at the main entry function so the
	// adapter→state mirror activity shows up in the trace tree.
	ctx, span := s.tracer.Start(ctx, "adaptersync.sync")
	defer span.End()
	// MinPollInterval gate (#888): zero-value lastPoll fires the first
	// call; subsequent calls inside the budget window short-circuit
	// BEFORE the network hop. lastPoll advances on the error path too so
	// a flapping rate-limited adapter is not retried-into-the-throttle.
	if minPoll := s.adapter.Capabilities().MinPollInterval; minPoll > 0 && !s.lastPoll.IsZero() && pollStartedAt.Sub(s.lastPoll) < minPoll {
		return nil
	}
	s.lastPoll = pollStartedAt
	items, err := s.adapter.List(ctx)
	if err != nil {
		s.adapterPollErrors.Add(ctx, 1)
		return fmt.Errorf("adaptersync: adapter list: %w", err)
	}

	if len(items) == 0 {
		s.log.Warn("adapter.empty_list", "cutoff", pollStartedAt, "skipping_tombstone", true)
		return s.cascadeChildrenOfArchivedPrograms(ctx, pollStartedAt)
	}

	seen := map[string]bool{}
	staged := make([]state.WorkItem, 0, len(items))
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := string(it.ID)
		if seen[id] {
			s.log.Warn("adapter.duplicate_id", "id", id)
			continue
		}
		seen[id] = true

		kind, ok := mapAdapterKind(it.Kind)
		if !ok {
			s.log.Warn("adapter.item_skipped", "id", id, "reason", "unknown_kind", "value", string(it.Kind))
			continue
		}
		status, ok := mapAdapterStatus(it.Status)
		if !ok {
			s.log.Warn("adapter.item_skipped", "id", id, "reason", "unknown_status", "value", string(it.Status))
			continue
		}
		lane := string(it.Lane)
		if lane == "" {
			s.log.Warn("adapter.item_skipped", "id", id, "reason", "empty_lane")
			continue
		}

		staged = append(staged, state.WorkItem{
			ID:     id,
			Kind:   kind,
			Title:  it.Title,
			Lane:   lane,
			Status: status,
		})
	}
	if err := s.db.BatchUpsertWorkItems(ctx, staged, state.SourceAdapter, pollStartedAt); err != nil {
		return fmt.Errorf("adaptersync: batch upsert: %w", err)
	}

	archived, err := s.db.TombstoneBySource(ctx, state.SourceAdapter, pollStartedAt)
	if err != nil {
		return fmt.Errorf("adaptersync: tombstone: %w", err)
	}
	for _, id := range archived {
		s.log.Warn("adapter.tombstoned", "id", id, "cutoff", pollStartedAt)
	}
	return s.cascadeChildrenOfArchivedPrograms(ctx, pollStartedAt)
}

// cascadeChildrenOfArchivedPrograms archives live children of any
// already-archived program. Idempotent recovery for a tick that
// archived a parent but crashed before fanning out. Emits one
// child.cascade_archived per child so operators can grep by child id.
func (s *Syncer) cascadeChildrenOfArchivedPrograms(ctx context.Context, at time.Time) error {
	orphans, err := s.db.ListArchivedProgramsWithLiveChildren(ctx)
	if err != nil {
		return fmt.Errorf("adaptersync: list orphaned programs: %w", err)
	}
	for _, parentID := range orphans {
		if err := ctx.Err(); err != nil {
			return err
		}
		archived, err := s.db.CascadeArchiveChildren(ctx, parentID, at)
		if err != nil {
			return fmt.Errorf("adaptersync: cascade %s: %w", parentID, err)
		}
		for _, childID := range archived {
			s.log.Warn("child.cascade_archived", "child", childID, "parent", parentID, "cutoff", at)
		}
	}
	return nil
}

// mapAdapterKind translates schemas.Kind* → state.Kind*. ok=false on
// any unrecognized value so Sync can skip + warn instead of casting
// garbage into the DB.
func mapAdapterKind(k schemas.WorkItemKind) (state.WorkItemKind, bool) {
	switch k {
	case schemas.KindFeature:
		return state.KindFeature, true
	case schemas.KindProgram:
		return state.KindProgram, true
	default:
		return "", false
	}
}

// mapAdapterStatus translates the read-only adapter Status to the
// universal-queue Status. Only `planned` maps; in_progress / done
// belong to the agent state-machine and never originate from adapters.
func mapAdapterStatus(s schemas.Status) (state.WorkItemStatus, bool) {
	switch s {
	case schemas.StatusPlanned:
		return state.WorkStatusPlanned, true
	default:
		return "", false
	}
}

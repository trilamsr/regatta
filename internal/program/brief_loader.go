// Package program — BriefLoader scans .regatta/programs/*.json,
// verifies each brief, and upserts child work_items rows.
//
// Sign-then-persist (spec §2.4 + §3): a brief failing Validate or
// VerifySignature emits obs.EventBriefRejected through the injected
// logger and no child rows touch state. Issue #80 adds a parallel
// durable audit sink so every rejection slog.Warn is followed by a
// best-effort substrate `brief_rejected` row.
package program

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// auditNowFunc — clock recordBriefRejection uses for substrate stamps.
type auditNowFunc = func() time.Time

// maxCascadeIterations bounds the fixed-point loop in
// reconcileDependencyArchive so a corrupt depends_on_features graph
// cannot wedge the orchestrator. Well-formed data converges in
// O(max-chain-depth).
const maxCascadeIterations = 1000

// BriefLoaderConfig holds BriefLoader deps; mirrors the Config.Logger
// DI pattern shared across orchestrator/scheduler/reaper.
type BriefLoaderConfig struct {
	// FS is the briefs directory — typically
	// os.DirFS(filepath.Join(repoRoot, ".regatta", "programs")) in
	// production, fstest.MapFS in tests. Required.
	FS fs.FS

	// DB is the universal state store. Required.
	DB *state.DB

	// Keyring maps key-id → HMAC secret for VerifySignature. Required.
	Keyring map[string][]byte

	// Evaluator is the shared *EdgeEvaluator handed to the scheduler.
	// Sync warms its compile cache on v2 edges; nil skips the warm
	// pass (legal for v1-only deployments and tests).
	Evaluator *EdgeEvaluator

	// Logger sinks brief-rejection diagnostics; nil falls back to
	// slog.Default().
	Logger *slog.Logger

	// Tracer falls back to otel.Tracer("program") — noop until
	// obs/otel.Setup runs (W6 spec §3.3).
	Tracer trace.Tracer

	// Meter resolves lazily at ResolveMeter() so a global provider
	// swap takes effect on the next call.
	Meter metric.Meter

	// Audit is the substrate-sink config for durable rejection records
	// (#80). Zero value disables — an operator without REGATTA_HMAC_KEY
	// keeps slog-only behaviour.
	Audit BriefAuditConfig
}

// ResolveMeter returns the configured meter or falls back lazily.
func (c BriefLoaderConfig) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeProgram)
}

// BriefLoader is the recurring sync — construct once at orchestrator
// boot; Sync once per PollOnce tick. outputsSchemas + programByFeature
// are the live brief-load projection the scheduler reads; they are
// rebuilt from scratch on every Sync so a re-plan that drops feature
// F-X removes F-X's schema entry by next tick.
type BriefLoader struct {
	fsys      fs.FS
	db        *state.DB
	keyring   map[string][]byte
	evaluator *EdgeEvaluator
	log       *slog.Logger
	tracer    trace.Tracer
	audit    BriefAuditConfig
	auditNow auditNowFunc

	mu               sync.RWMutex
	outputsSchemas   map[FeatureID]*OutputsSchema
	programByFeature map[FeatureID]string
}

// NewBriefLoader returns an error when any required field (FS, DB,
// Keyring) is nil — surfaces misconfiguration at boot instead of
// deferring a nil-deref panic to the first Sync. Timestamps are
// threaded through Sync's pollStartedAt rather than held here, mirroring
// the AdapterSync DI pattern.
func NewBriefLoader(cfg BriefLoaderConfig) (*BriefLoader, error) {
	if cfg.FS == nil {
		return nil, fmt.Errorf("program: BriefLoaderConfig.FS is required")
	}
	if cfg.DB == nil {
		return nil, fmt.Errorf("program: BriefLoaderConfig.DB is required")
	}
	if cfg.Keyring == nil {
		return nil, fmt.Errorf("program: BriefLoaderConfig.Keyring is required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("program")
	}
	return &BriefLoader{
		fsys:             cfg.FS,
		db:               cfg.DB,
		keyring:          cfg.Keyring,
		evaluator:        cfg.Evaluator,
		log:              log,
		tracer:           tracer,
		audit:            cfg.Audit,
		auditNow:         time.Now,
		outputsSchemas:   map[FeatureID]*OutputsSchema{},
		programByFeature: map[FeatureID]string{},
	}, nil
}

// OutputsSchemaForFeature returns the most-recent successful brief
// Sync's OutputsSchema for featureID; (nil, false) when no brief has
// declared a schema. Scheduler-side adapter boxes the return as `any`.
func (b *BriefLoader) OutputsSchemaForFeature(id FeatureID) (*OutputsSchema, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	sch, ok := b.outputsSchemas[id]
	return sch, ok
}

// Sync globs *.json in fsys (skipping *.tmp), verifies each, and
// upserts child WorkItems. Rows whose last_seen_at predates
// pollStartedAt and source=brief are tombstoned at the end of the
// loop. pollStartedAt is the single timestamp source for every write
// — DB-clock injection is forbidden in production paths.
//
// Cross-brief feature-ID collisions are first-writer-wins: the
// alphabetically-first brief that claims a feature ID gets it; later
// briefs trying to redefine the same ID under a different parent are
// skipped + warned. Glob output is sort.Strings'd so collision
// resolution stays deterministic.
//
// Briefs whose ParentWorkItemID does not resolve to an existing
// work_items row are rejected (orphan-prevention). Replay defense:
// briefs whose ProducedAt is <= MAX(updated_at) over existing brief
// children of the program are rejected stale_produced_at. Survives
// restart because the watermark is derived from durable state.
func (b *BriefLoader) Sync(ctx context.Context, pollStartedAt time.Time) error {
	// W6 spec §8 T5: open a span at the main entry function.
	ctx, span := b.tracer.Start(ctx, "brief_loader.sync")
	defer span.End()
	entries, err := fs.Glob(b.fsys, "*.json")
	if err != nil {
		return fmt.Errorf("brief_loader: glob: %w", err)
	}
	sort.Strings(entries)

	// Stage v2 OutputsSchemas in a per-sync scratch map so a brief
	// rejected mid-loop never leaks into the resolver, and features
	// dropped from the new sync vanish from the resolver when we swap
	// at the end. The active map is only replaced on success so a
	// panic during loop body leaves the prior view intact.
	freshSchemas := map[FeatureID]*OutputsSchema{}
	freshProgramBy := map[FeatureID]string{}

	seenFeature := map[string]string{}
	for _, path := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		// *.json glob already filters; belt-and-suspenders for atypical
		// atomic-write patterns like foo.tmp.json.
		if strings.HasSuffix(path, ".tmp") {
			continue
		}
		brief, v2, err := loadAndVerifyBriefBoth(b.fsys, path, b.keyring)
		if err != nil {
			b.log.Warn(string(obs.EventBriefRejected), "path", path, "reason", err.Error())
			b.recordBriefRejection(ctx, path, err.Error())
			continue
		}
		if _, err := b.db.GetWorkItem(ctx, brief.ParentWorkItemID); err != nil {
			if errors.Is(err, state.ErrWorkItemNotFound) {
				b.log.Warn(string(obs.EventBriefRejected), "path", path,
					"reason", "unknown_parent_program", "id", brief.ParentWorkItemID)
				b.recordBriefRejection(ctx, path, "unknown_parent_program: "+brief.ParentWorkItemID)
				continue
			}
			return fmt.Errorf("brief_loader: probe parent %s: %w", brief.ParentWorkItemID, err)
		}
		// Issue #92: processed_briefs is the durable replay-defence
		// floor; work_items watermark is fallback-only because the row
		// set is mutable by operators. Two-layer probe so an exact MAC
		// re-presentation is rejected even at equal/earlier produced_at.
		if seen, err := b.db.HasProcessedBriefHMAC(ctx, brief.Signature.MAC); err != nil {
			return fmt.Errorf("brief_loader: probe hmac for %s: %w", brief.ParentWorkItemID, err)
		} else if seen {
			b.log.Warn(string(obs.EventBriefRejected), "path", path,
				"reason", "brief_already_processed",
				"parent", brief.ParentWorkItemID)
			b.recordBriefRejection(ctx, path, "brief_already_processed: "+brief.ParentWorkItemID)
			continue
		}
		recorded, hasRecorded, err := b.db.GetProcessedBrief(ctx, brief.ParentWorkItemID)
		if err != nil {
			return fmt.Errorf("brief_loader: probe processed_briefs for %s: %w", brief.ParentWorkItemID, err)
		}
		watermark, err := b.db.MaxUpdatedAtForBriefChildren(ctx, brief.ParentWorkItemID)
		if err != nil {
			return fmt.Errorf("brief_loader: freshness watermark for %s: %w", brief.ParentWorkItemID, err)
		}
		// MAX over both layers — durable floor cannot regress when
		// operators delete work_items rows (the original #92 attack).
		if hasRecorded && recorded.LastProducedAt.After(watermark) {
			watermark = recorded.LastProducedAt
		}
		if !watermark.IsZero() && !brief.ProducedAt.After(watermark) {
			b.log.Warn(string(obs.EventBriefRejected), "path", path,
				"reason", "stale_produced_at",
				"produced_at", brief.ProducedAt, "watermark", watermark)
			b.recordBriefRejection(ctx, path, fmt.Sprintf("stale_produced_at: produced_at=%s watermark=%s",
				brief.ProducedAt.Format(time.RFC3339), watermark.Format(time.RFC3339)))
			continue
		}

		acceptanceByFulfilled := map[string]string{}
		for _, c := range brief.ParentCriteria {
			acceptanceByFulfilled[c.ID] = c.Text
		}
		// Stage children, flush in one BatchUpsertWorkItems (#89).
		// Per-brief flush preserves the cross-brief CycleCheck contract
		// — next brief sees prior brief's rows before its own
		// CycleCheck. Intra-brief cycles are rejected by brief.Validate
		// before this loop.
		staged := make([]state.WorkItem, 0, len(brief.Features))
		for _, feat := range brief.Features {
			if firstPath, dup := seenFeature[feat.ID]; dup {
				b.log.Warn(string(obs.EventBriefRejected), "path", path,
					"reason", "feature_id_collision",
					"feature", feat.ID, "first_seen_in", firstPath)
				b.recordBriefRejection(ctx, path, "feature_id_collision: "+feat.ID+" first_seen_in="+firstPath)
				continue
			}
			seenFeature[feat.ID] = path

			snapshot, mErr := json.Marshal(featureAcceptanceSnapshot(feat, acceptanceByFulfilled))
			if mErr != nil {
				return fmt.Errorf("brief_loader: marshal snapshot for %s: %w", feat.ID, mErr)
			}
			// Issue #78: warn when a re-loaded brief's acceptance
			// diverges from an in-flight child's snapshot. Snapshot
			// semantics stay intentional (spec §2.4 + §2.5 Locked #5 —
			// no auto-resync); this is the visibility seam so silent
			// re-snapshots no longer surprise agents whose gate bar
			// just shifted. Skip planned + archived: planned snaps
			// forward harmlessly; archived no longer gates.
			if existing, gErr := b.db.GetWorkItem(ctx, feat.ID); gErr == nil &&
				existing.Status != state.WorkStatusPlanned &&
				existing.Status != state.WorkStatusArchived &&
				existing.AcceptanceJSON != "" &&
				existing.AcceptanceJSON != string(snapshot) {
				b.log.Warn(string(obs.EventBriefCriteriaDrift),
					string(obs.KeyProgramID), brief.ParentWorkItemID,
					string(obs.KeyWorkItemID), feat.ID,
					"prior_status", string(existing.Status),
					"path", path)
			}
			child := state.WorkItem{
				ID:                feat.ID,
				Kind:              state.KindFeature,
				Title:             feat.Title,
				Lane:              "server", // MVP-1 single-lane; multi-lane is MVP-2
				Status:            state.WorkStatusPlanned,
				ParentProgramID:   brief.ParentWorkItemID,
				DependsOnFeatures: feat.DependsOnFeatures,
				AcceptanceJSON:    string(snapshot),
			}
			if cycErr := b.db.CycleCheck(ctx, child); cycErr != nil {
				b.log.Warn(string(obs.EventBriefRejected), "path", path, "reason", cycErr.Error())
				b.recordBriefRejection(ctx, path, cycErr.Error())
				continue
			}
			staged = append(staged, child)
		}
		if err := b.db.BatchUpsertWorkItems(ctx, staged, state.SourceBrief, pollStartedAt); err != nil {
			return fmt.Errorf("brief_loader: batch upsert children of %s: %w", brief.ParentWorkItemID, err)
		}
		if v2 != nil {
			if err := b.materialiseEdges(ctx, v2, pollStartedAt); err != nil {
				return err
			}
			for i := range v2.FeaturesV2 {
				f := &v2.FeaturesV2[i]
				if f.OutputsSchema == nil {
					continue
				}
				freshSchemas[f.ID] = f.OutputsSchema
				freshProgramBy[f.ID] = v2.ProgramID
			}
		}
		// #92: persist replay-defence watermark independent of
		// work_items so deleting brief children cannot reset the floor.
		// Written last so a partial-failure brief never records as
		// accepted.
		if err := b.db.RecordProcessedBrief(ctx, brief.ParentWorkItemID,
			brief.ProducedAt, brief.Signature.MAC, pollStartedAt); err != nil {
			return fmt.Errorf("brief_loader: record processed_brief %s: %w", brief.ParentWorkItemID, err)
		}
	}

	b.mu.Lock()
	b.outputsSchemas = freshSchemas
	b.programByFeature = freshProgramBy
	b.mu.Unlock()

	archived, err := b.db.TombstoneBySource(ctx, state.SourceBrief, pollStartedAt)
	if err != nil {
		return fmt.Errorf("brief_loader: tombstone: %w", err)
	}
	for _, id := range archived {
		b.log.Warn("brief.tombstoned", "id", id, "cutoff", pollStartedAt)
	}

	if err := b.reconcileDependencyArchive(ctx, pollStartedAt); err != nil {
		return err
	}
	return nil
}

// reconcileDependencyArchive walks children whose depends_on_features
// references a tombstoned sibling and cascade-archives them.
// Idempotent + Sync-independent: safe to call repeatedly with no
// producer activity. Loops to a fixed point so chain
// A(archived) → B → C converges in one Sync rather than N ticks.
// maxCascadeIterations guards against corrupt graphs.
func (b *BriefLoader) reconcileDependencyArchive(ctx context.Context, at time.Time) error {
	for i := 0; i < maxCascadeIterations; i++ {
		applied, err := b.cascadeDependencyArchiveOnce(ctx, at)
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
	}
	return orchestrator.ErrCascadeNonConverging
}

// cascadeDependencyArchiveOnce executes one pass over live brief
// children and archives any whose depends_on_features includes an
// already-archived sibling. Returns true on any new archive (drives
// the fixed-point loop).
func (b *BriefLoader) cascadeDependencyArchiveOnce(ctx context.Context, at time.Time) (bool, error) {
	rows, err := b.db.SQL().QueryContext(ctx, `
		SELECT id, depends_on_features FROM work_items
		WHERE source = ? AND status != ?`,
		string(state.SourceBrief), string(state.WorkStatusArchived))
	if err != nil {
		return false, fmt.Errorf("brief_loader: scan deps: %w", err)
	}
	type pending struct {
		id   string
		deps []string
	}
	var rowsList []pending
	scanErr := func() error {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, depsJSON string
			if err := rows.Scan(&id, &depsJSON); err != nil {
				return err
			}
			var deps []string
			if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
				return err
			}
			rowsList = append(rowsList, pending{id, deps})
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return false, scanErr
	}

	applied := false
	for _, r := range rowsList {
		for _, dep := range r.deps {
			depItem, err := b.db.GetWorkItem(ctx, dep)
			if err != nil {
				if errors.Is(err, state.ErrWorkItemNotFound) {
					continue
				}
				return false, err
			}
			if depItem.Status != state.WorkStatusArchived {
				continue
			}
			// Fetch the child's current status to CAS-from. The outer
			// SELECT excluded status=archived, but two BriefLoader
			// passes can race on the same row: the typed transition
			// wins once, the loser sees ErrInvalidWorkItemTransition
			// and treats the row as already archived (idempotent
			// against the fixed-point retry loop).
			child, err := b.db.GetWorkItem(ctx, r.id)
			if err != nil {
				if errors.Is(err, state.ErrWorkItemNotFound) {
					continue
				}
				return false, err
			}
			if child.Status == state.WorkStatusArchived {
				continue
			}
			if err := b.db.TransitionWorkItemAt(ctx, r.id, child.Status, state.WorkStatusArchived, at); err != nil {
				if errors.Is(err, state.ErrInvalidWorkItemTransition) {
					continue
				}
				return false, err
			}
			applied = true
			b.log.Warn("child.dependency_archived", "child", r.id, "dep", dep)
			break
		}
	}
	return applied, nil
}

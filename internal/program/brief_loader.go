// Package program — BriefLoader scans .regatta/programs/*.json,
// verifies each brief, and upserts child work_items rows.
//
// per spec §2.4 + §3 sign-then-persist: a brief that fails Validate
// or VerifySignature emits obs.EventBriefRejected through the
// injected logger and no child rows touch state. Rejections never
// enter sqlite; audit trail lives in logs (RFC-0001 §audit deferral
// to MVP-3+).
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
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// maxBriefSize caps the bytes BriefLoader will read for any single
// brief. 1 MiB is ~3 orders of magnitude above a realistic ProgramBrief
// (a handful of features with short titles) and bounds the OOM blast
// radius of a malicious or corrupt drop. Rejected at Stat() — never
// reaches json.Unmarshal.
const maxBriefSize = 1 << 20

// maxCascadeIterations bounds the fixed-point loop in
// reconcileDependencyArchive so a corrupt depends_on_features graph
// cannot wedge the orchestrator. With well-formed data the loop
// converges in O(max-chain-depth) iterations.
const maxCascadeIterations = 1000

// LoadAndVerifyBrief reads path from fsys, unmarshals into
// ProgramBrief, runs ProgramBrief.Validate, then VerifySignature
// under keyring. Returns ErrHMACInvalid (wrapped) when the
// signature does not check out under any key. Rejects briefs whose
// on-disk size exceeds maxBriefSize before any read into RAM.
//
// For v2 briefs the function routes through LoadAndVerifyBriefV2,
// then projects the v2 features into the v1 ProgramBrief.Features
// slice so downstream Sync (which only consults v1 fields) operates
// unchanged. The Edge → DependsOnFeatures projection mirrors
// LowerV1ToV2's inverse and preserves the dependency closure.
func LoadAndVerifyBrief(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := readBriefBytes(fsys, path)
	if err != nil {
		return nil, err
	}
	if IsV2Brief(raw) {
		v2, err := loadAndVerifyV2FromBytes(raw, keyring)
		if err != nil {
			return nil, err
		}
		return projectV2ToV1(v2), nil
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return &brief, nil
}

// loadAndVerifyBriefBoth returns both the v1-projected brief (for the
// existing v1-shaped Sync pipeline) and, when the source brief was v2,
// the raw v2 view (so Sync can lower edges + outputs_schemas without
// re-reading the file). v1 briefs return (brief, nil, nil).
//
// Centralising this here keeps the v2 detection + verification logic in
// one place — LoadAndVerifyBrief continues to satisfy callers that only
// need the v1 projection.
func loadAndVerifyBriefBoth(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, *ProgramBriefV2, error) {
	if len(keyring) == 0 {
		return nil, nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := readBriefBytes(fsys, path)
	if err != nil {
		return nil, nil, err
	}
	if IsV2Brief(raw) {
		v2, err := loadAndVerifyV2FromBytes(raw, keyring)
		if err != nil {
			return nil, nil, err
		}
		return projectV2ToV1(v2), v2, nil
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return &brief, nil, nil
}

// LoadAndVerifyBriefV2 is the v2-typed sibling of LoadAndVerifyBrief.
// v1 briefs are lowered via LowerV1ToV2 so callers see a single
// representation; v2 briefs flow through ValidateV2 + signature
// verification untouched.
func LoadAndVerifyBriefV2(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBriefV2, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := readBriefBytes(fsys, path)
	if err != nil {
		return nil, err
	}
	if IsV2Brief(raw) {
		return loadAndVerifyV2FromBytes(raw, keyring)
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return LowerV1ToV2(&brief), nil
}

func readBriefBytes(fsys fs.FS, path string) ([]byte, error) {
	info, err := fs.Stat(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("program: stat brief: %w", err)
	}
	if info.Size() > maxBriefSize {
		return nil, fmt.Errorf("program: brief %s size %d exceeds cap %d", path, info.Size(), maxBriefSize)
	}
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("program: read brief: %w", err)
	}
	return raw, nil
}

// loadAndVerifyV2FromBytes parses raw as v2, runs ValidateV2, then
// verifies HMAC against the canonicalised payload. HMAC verification
// reuses the v1 signature scheme: marshal to JSON, decode into a
// generic map, run schemas.Verify on that map. Because the embedded
// ProgramBrief carries the Signature field, schemas.Verify operates
// on the same canonical body the v1 path uses.
func loadAndVerifyV2FromBytes(raw []byte, keyring map[string][]byte) (*ProgramBriefV2, error) {
	var v2 ProgramBriefV2
	if err := json.Unmarshal(raw, &v2); err != nil {
		return nil, fmt.Errorf("program: parse v2 brief: %w", err)
	}
	if err := v2.ValidateV2(); err != nil {
		return nil, fmt.Errorf("program: validate v2 brief: %w", err)
	}
	if err := v2.VerifySignatureV2(keyring); err != nil {
		return nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return &v2, nil
}

// projectV2ToV1 fills the embedded ProgramBrief.Features slice from
// FeaturesV2, translating Edges into DependsOnFeatures entries so the
// existing v1-shaped Sync pipeline accepts a v2 brief unchanged.
// Outputs schema + predicate metadata is preserved on the returned
// ProgramBriefV2 (callers needing v2 fields use LoadAndVerifyBriefV2).
//
// V2 edges use outgoing semantics (e.From == owning feature ID). To
// reconstruct the v1 incoming-edge DependsOnFeatures view we reverse-
// index: for each edge U -> D in U.Edges, append U to D's
// DependsOnFeatures.
func projectV2ToV1(v2 *ProgramBriefV2) *ProgramBrief {
	out := v2.ProgramBrief
	out.Features = make([]PlannedFeature, len(v2.FeaturesV2))
	idxByID := make(map[string]int, len(v2.FeaturesV2))
	for i, f := range v2.FeaturesV2 {
		out.Features[i] = f.PlannedFeature
		idxByID[f.ID] = i
	}
	seenDep := make([]map[string]bool, len(v2.FeaturesV2))
	for i := range out.Features {
		seenDep[i] = map[string]bool{}
		for _, d := range out.Features[i].DependsOnFeatures {
			seenDep[i][d] = true
		}
	}
	for _, f := range v2.FeaturesV2 {
		for _, e := range f.Edges {
			j, ok := idxByID[e.To]
			if !ok {
				continue
			}
			if seenDep[j][e.From] {
				continue
			}
			out.Features[j].DependsOnFeatures = append(out.Features[j].DependsOnFeatures, e.From)
			seenDep[j][e.From] = true
		}
	}
	return &out
}

// BriefLoaderConfig holds dependencies for a BriefLoader. Mirrors the
// Config.Logger DI pattern used by orchestrator, scheduler, and reaper
// so all components share one injection shape.
type BriefLoaderConfig struct {
	// FS is the briefs directory. Typically
	// os.DirFS(filepath.Join(repoRoot, ".regatta", "programs")) in
	// production and fstest.MapFS in tests. Required.
	FS fs.FS

	// DB is the universal state store. Required.
	DB *state.DB

	// Keyring maps key-id → HMAC secret for VerifySignature. Required.
	Keyring map[string][]byte

	// Evaluator is the shared *EdgeEvaluator handed to the scheduler.
	// Sync warms its compile cache on v2 edges; nil skips the warm
	// pass (legal for v1-only deployments and tests).
	Evaluator *EdgeEvaluator

	// Logger is the structured-event sink for brief-rejection
	// diagnostics. Nil falls back to slog.Default() so embedded
	// callers still get output without panicking (spec §4.1 + §5.7).
	Logger *slog.Logger

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("program") which resolves to the
	// global provider — noop until obs/otel.Setup runs. Per W6 spec
	// §3.3 + feedback_spec_pattern_authority.
	Tracer trace.Tracer
}

// BriefLoader is the recurring sync. Construct once at orchestrator
// boot; Sync once per PollOnce tick.
//
// outputsSchemas + programByFeature are the live brief-load projection
// the scheduler's OutputsSchemaResolver reads. They are rebuilt from
// scratch on every Sync so a re-plan that drops feature F-X removes
// F-X's schema entry by next tick (cross-brief staleness defence).
type BriefLoader struct {
	fsys      fs.FS
	db        *state.DB
	keyring   map[string][]byte
	evaluator *EdgeEvaluator
	log       *slog.Logger
	tracer    trace.Tracer

	mu               sync.RWMutex
	outputsSchemas   map[FeatureID]*OutputsSchema
	programByFeature map[FeatureID]string
}

// NewBriefLoader constructs a BriefLoader from a BriefLoaderConfig.
// Returns an error if any required field (FS, DB, Keyring) is nil —
// surfacing misconfiguration at boot instead of deferring a nil-deref
// panic to the first Sync call. Timestamps are threaded through Sync's
// pollStartedAt rather than held here, mirroring the AdapterSync DI
// pattern — concurrent producers cannot race on a constructor-bound
// clock.
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
		outputsSchemas:   map[FeatureID]*OutputsSchema{},
		programByFeature: map[FeatureID]string{},
	}, nil
}

// OutputsSchemaForFeature returns the OutputsSchema declared by the
// most-recent successful brief Sync for featureID. (nil, false) when
// no brief has declared a schema for that feature — schedulers treat
// this as "no schema", matching the runtime evaluator's contract that
// schema is opaque at the runtime seam (type-check is at brief-load).
//
// Production wires this into scheduler.Config.OutputsSchemas via an
// adapter closure in cmd/regatta that boxes the *OutputsSchema return
// as the scheduler-side `any`.
func (b *BriefLoader) OutputsSchemaForFeature(id FeatureID) (*OutputsSchema, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	sch, ok := b.outputsSchemas[id]
	return sch, ok
}

// Sync globs *.json in fsys (skipping *.tmp), verifies each, and
// upserts child WorkItems for the brief's features. Rows whose
// last_seen_at predates pollStartedAt and source=brief are
// tombstoned at the end of the loop. pollStartedAt is the single
// timestamp source for every write — DB-clock injection is forbidden
// in production paths.
//
// Cross-brief feature-ID collisions are first-writer-wins: the
// alphabetically-first brief that claims a feature ID gets it; later
// briefs trying to redefine the same ID under a different parent are
// skipped + warned. Glob output is sort.Strings'd so collision
// resolution is deterministic.
//
// Briefs whose ParentWorkItemID does not resolve to an existing
// work_items row are rejected (orphan-prevention).
//
// Replay defense: briefs whose ProducedAt is <= MAX(updated_at) over
// existing brief children of the program are rejected with reason
// stale_produced_at. This survives orchestrator restart because the
// watermark is derived from durable state.
func (b *BriefLoader) Sync(ctx context.Context, pollStartedAt time.Time) error {
	// W6 spec §8 T5: open a span at the main entry function so brief
	// sync activity shows up in the trace tree.
	ctx, span := b.tracer.Start(ctx, "brief_loader.sync")
	defer span.End()
	entries, err := fs.Glob(b.fsys, "*.json")
	if err != nil {
		return fmt.Errorf("brief_loader: glob: %w", err)
	}
	sort.Strings(entries)

	// Stage v2 OutputsSchemas in a per-sync scratch map so a brief
	// rejected mid-loop never leaks into the resolver, and features
	// dropped from the new sync's brief set vanish from the resolver
	// when we swap maps at the end. The active map is only replaced on
	// the success path so a panic during loop body leaves the prior
	// view intact.
	freshSchemas := map[FeatureID]*OutputsSchema{}
	freshProgramBy := map[FeatureID]string{}

	seenFeature := map[string]string{}
	for _, path := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		// *.json glob already filters; belt-and-suspenders for
		// atypical atomic-write patterns like foo.tmp.json.
		if strings.HasSuffix(path, ".tmp") {
			continue
		}
		brief, v2, err := loadAndVerifyBriefBoth(b.fsys, path, b.keyring)
		if err != nil {
			b.log.Warn(string(obs.EventBriefRejected), "path", path, "reason", err.Error())
			continue
		}
		if _, err := b.db.GetWorkItem(ctx, brief.ParentWorkItemID); err != nil {
			if errors.Is(err, state.ErrWorkItemNotFound) {
				b.log.Warn(string(obs.EventBriefRejected), "path", path,
					"reason", "unknown_parent_program", "id", brief.ParentWorkItemID)
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
		// MAX over both layers — the durable floor cannot regress when
		// work_items rows are deleted (the original #92 attack).
		if hasRecorded && recorded.LastProducedAt.After(watermark) {
			watermark = recorded.LastProducedAt
		}
		if !watermark.IsZero() && !brief.ProducedAt.After(watermark) {
			b.log.Warn(string(obs.EventBriefRejected), "path", path,
				"reason", "stale_produced_at",
				"produced_at", brief.ProducedAt, "watermark", watermark)
			continue
		}

		acceptanceByFulfilled := map[string]string{}
		for _, c := range brief.ParentCriteria {
			acceptanceByFulfilled[c.ID] = c.Text
		}
		for _, feat := range brief.Features {
			if firstPath, dup := seenFeature[feat.ID]; dup {
				b.log.Warn(string(obs.EventBriefRejected), "path", path,
					"reason", "feature_id_collision",
					"feature", feat.ID, "first_seen_in", firstPath)
				continue
			}
			seenFeature[feat.ID] = path

			snapshot, mErr := json.Marshal(featureAcceptanceSnapshot(feat, acceptanceByFulfilled))
			if mErr != nil {
				return fmt.Errorf("brief_loader: marshal snapshot for %s: %w", feat.ID, mErr)
			}
			child := state.WorkItem{
				ID:                feat.ID,
				Kind:              state.KindFeature,
				Title:             feat.Title,
				Lane:              "server", // MVP-1: single-lane; spec §out-of-scope notes multi-lane is MVP-2
				Status:            state.WorkStatusPlanned,
				ParentProgramID:   brief.ParentWorkItemID,
				DependsOnFeatures: feat.DependsOnFeatures,
				AcceptanceJSON:    string(snapshot),
			}
			if cycErr := b.db.CycleCheck(ctx, child); cycErr != nil {
				b.log.Warn(string(obs.EventBriefRejected), "path", path, "reason", cycErr.Error())
				continue
			}
			if upErr := b.db.UpsertWorkItem(ctx, child, state.SourceBrief, pollStartedAt); upErr != nil {
				return fmt.Errorf("brief_loader: upsert %s: %w", feat.ID, upErr)
			}
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
		// Issue #92: persist replay-defence watermark independent of
		// work_items so an operator deleting brief children cannot reset
		// the floor to zero. Written last so a partial-failure brief
		// (any return above) never records as accepted.
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

// materialiseEdges lowers a v2 brief's Edges + DefaultNext into
// work_item_edges rows and upserts them in one transaction (per
// UpsertEdgesAt's contract). v1 briefs have no edge data and skip
// this pass entirely.
//
// Existing rows preserve their fired/fired_against state — re-plans
// that mutate predicate text refresh predicate_cel + on_skip but the
// scheduler's eval result for the prior predicate stays put. Operators
// who want a clean re-evaluation tombstone the program manually.
func (b *BriefLoader) materialiseEdges(ctx context.Context, v2 *ProgramBriefV2, at time.Time) error {
	if v2 == nil {
		return nil
	}
	var rows []state.EdgeRow
	for _, f := range v2.FeaturesV2 {
		for _, e := range f.Edges {
			rows = append(rows, state.EdgeRow{
				ProgramID:    v2.ProgramID,
				FromID:       e.From,
				ToID:         e.To,
				PredicateCEL: e.Predicate,
				IsDefault:    false,
				OnSkip:       string(skipOrDefault(e.OnSkip)),
			})
		}
		if f.DefaultNext != nil {
			rows = append(rows, state.EdgeRow{
				ProgramID:    v2.ProgramID,
				FromID:       f.ID,
				ToID:         *f.DefaultNext,
				PredicateCEL: "",
				IsDefault:    true,
				OnSkip:       string(SkipCascade),
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if err := b.db.UpsertEdgesAt(ctx, v2.ProgramID, rows, at); err != nil {
		return fmt.Errorf("brief_loader: upsert edges for %s: %w", v2.ProgramID, err)
	}
	b.log.Info(string(obs.EventBriefEdgesMaterialised),
		string(obs.KeyProgramID), v2.ProgramID, "count", len(rows))
	return nil
}

// skipOrDefault canonicalises an empty SkipMode to SkipCascade so
// every persisted edge row carries a non-empty on_skip value.
func skipOrDefault(s SkipMode) SkipMode {
	if s == "" {
		return SkipCascade
	}
	return s
}

// featureAcceptanceSnapshot snapshots the criteria a feature fulfills
// at brief-ingestion time so downstream cascade-soft archival keeps the
// child self-describing — operators reading a stale archived row can
// still see what acceptance bar it was meant to clear (per spec §2.4).
func featureAcceptanceSnapshot(f PlannedFeature, byFulfilled map[string]string) []PlanCriterion {
	out := make([]PlanCriterion, 0, len(f.Fulfills))
	for _, fid := range f.Fulfills {
		out = append(out, PlanCriterion{ID: fid, Text: byFulfilled[fid]})
	}
	return out
}

// reconcileDependencyArchive walks children whose depends_on_features
// references a tombstoned (archived) sibling and cascade-archives
// them. Idempotent + Sync-independent: safe to call repeatedly with
// no producer activity. Loops to a fixed point so a chain
// A(archived) -> B -> C converges in one Sync invocation rather than
// taking N ticks. Bounded by maxCascadeIterations to guard a corrupt
// graph from wedging the orchestrator.
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
// already-archived sibling. Returns true if any row was newly
// archived (driving the fixed-point loop).
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

	stamp := at.UTC().Unix()
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
			if _, err := b.db.SQL().ExecContext(ctx,
				`UPDATE work_items SET status = ?, updated_at = ? WHERE id = ?`,
				string(state.WorkStatusArchived), stamp, r.id); err != nil {
				return false, err
			}
			applied = true
			b.log.Warn("child.dependency_archived", "child", r.id, "dep", dep)
			break
		}
	}
	return applied, nil
}

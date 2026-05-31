// Package program — BriefLoader scans .regatta/programs/*.json,
// verifies each brief, and upserts child work_items rows.
//
// per spec §2.4 + §3 sign-then-persist: a brief that fails Validate
// or VerifySignature emits slog.Warn("brief.rejected") and no
// child rows touch state. Rejections never enter sqlite; audit
// trail lives in logs (RFC-0001 §audit deferral to MVP-3+).
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
	"time"

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
func LoadAndVerifyBrief(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
	}
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

// BriefLoader is the recurring sync. Construct once at orchestrator
// boot; Sync once per PollOnce tick.
type BriefLoader struct {
	fsys    fs.FS
	db      *state.DB
	keyring map[string][]byte
}

// NewBriefLoader constructs a BriefLoader. fsys is typically
// os.DirFS(filepath.Join(repoRoot, ".regatta", "programs")) in
// production and fstest.MapFS in tests. No clock argument: timestamps
// are threaded through Sync's pollStartedAt and passed into the
// explicit *At state APIs, mirroring the AdapterSync DI pattern from
// commit 3741f0a — concurrent producers can no longer race on a
// SetClock-installed clock.
func NewBriefLoader(fsys fs.FS, db *state.DB, keyring map[string][]byte) *BriefLoader {
	return &BriefLoader{fsys: fsys, db: db, keyring: keyring}
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
	entries, err := fs.Glob(b.fsys, "*.json")
	if err != nil {
		return fmt.Errorf("brief_loader: glob: %w", err)
	}
	sort.Strings(entries)

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
		brief, err := LoadAndVerifyBrief(b.fsys, path, b.keyring)
		if err != nil {
			slog.Warn("brief.rejected", "path", path, "reason", err.Error())
			continue
		}
		if _, err := b.db.GetWorkItem(ctx, brief.ParentWorkItemID); err != nil {
			if errors.Is(err, state.ErrWorkItemNotFound) {
				slog.Warn("brief.rejected", "path", path,
					"reason", "unknown_parent_program", "id", brief.ParentWorkItemID)
				continue
			}
			return fmt.Errorf("brief_loader: probe parent %s: %w", brief.ParentWorkItemID, err)
		}
		watermark, err := b.db.MaxUpdatedAtForBriefChildren(ctx, brief.ParentWorkItemID)
		if err != nil {
			return fmt.Errorf("brief_loader: freshness watermark for %s: %w", brief.ParentWorkItemID, err)
		}
		if !watermark.IsZero() && !brief.ProducedAt.After(watermark) {
			slog.Warn("brief.rejected", "path", path,
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
				slog.Warn("brief.rejected", "path", path,
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
				slog.Warn("brief.rejected", "path", path, "reason", cycErr.Error())
				continue
			}
			if upErr := b.db.UpsertWorkItemAt(ctx, child, state.SourceBrief, pollStartedAt); upErr != nil {
				return fmt.Errorf("brief_loader: upsert %s: %w", feat.ID, upErr)
			}
		}
	}

	archived, err := b.db.TombstoneBySourceAt(ctx, state.SourceBrief, pollStartedAt)
	if err != nil {
		return fmt.Errorf("brief_loader: tombstone: %w", err)
	}
	for _, id := range archived {
		slog.Warn("brief.tombstoned", "id", id, "cutoff", pollStartedAt)
	}

	if err := b.reconcileDependencyArchive(ctx, pollStartedAt); err != nil {
		return err
	}
	return nil
}

// featureAcceptanceSnapshot returns the subset of parent criteria
// this feature fulfills, in stable order.
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
			slog.Warn("child.dependency_archived", "child", r.id, "dep", dep)
			break
		}
	}
	return applied, nil
}

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
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// LoadAndVerifyBrief reads path from fsys, unmarshals into
// ProgramBrief, runs ProgramBrief.Validate, then VerifySignature
// under keyring. Returns ErrHMACInvalid (wrapped) when the
// signature does not check out under any key.
func LoadAndVerifyBrief(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
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
		return nil, fmt.Errorf("%w: %v", orchestrator.ErrHMACInvalid, err)
	}
	return &brief, nil
}

// BriefLoader is the recurring sync. Construct once at orchestrator
// boot; Sync once per PollOnce tick.
type BriefLoader struct {
	fsys    fs.FS
	db      *state.DB
	now     func() time.Time
	keyring map[string][]byte
}

// NewBriefLoader constructs a BriefLoader. fsys is typically
// os.DirFS(filepath.Join(repoRoot, ".regatta", "programs")) in
// production and fstest.MapFS in tests. Installs `now` on db so
// UpsertWorkItem's d.now()-stamped last_seen_at lines up with the
// pollStartedAt cutoff TombstoneBySource compares against — mirrors
// the adaptersync DI pattern in commit 4fdb53a.
func NewBriefLoader(fsys fs.FS, db *state.DB, now func() time.Time, keyring map[string][]byte) *BriefLoader {
	db.SetClock(now)
	return &BriefLoader{fsys: fsys, db: db, now: now, keyring: keyring}
}

// Sync globs *.json in fsys (skipping *.tmp), verifies each, and
// upserts child WorkItems for the brief's features. Rows whose
// last_seen_at predates pollStartedAt and source=brief are
// tombstoned at the end of the loop.
func (b *BriefLoader) Sync(ctx context.Context, pollStartedAt time.Time) error {
	entries, err := fs.Glob(b.fsys, "*.json")
	if err != nil {
		return fmt.Errorf("brief_loader: glob: %w", err)
	}

	for _, path := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasSuffix(path, ".tmp") {
			continue
		}
		brief, err := LoadAndVerifyBrief(b.fsys, path, b.keyring)
		if err != nil {
			slog.Warn("brief.rejected", "path", path, "reason", err.Error())
			continue
		}
		acceptanceByFulfilled := map[string]string{}
		for _, c := range brief.ParentCriteria {
			acceptanceByFulfilled[c.ID] = c.Text
		}
		for _, feat := range brief.Features {
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
			if upErr := b.db.UpsertWorkItem(ctx, child, state.SourceBrief); upErr != nil {
				return fmt.Errorf("brief_loader: upsert %s: %w", feat.ID, upErr)
			}
		}
	}

	archived, err := b.db.TombstoneBySource(ctx, state.SourceBrief, pollStartedAt)
	if err != nil {
		return fmt.Errorf("brief_loader: tombstone: %w", err)
	}
	for _, id := range archived {
		slog.Warn("brief.tombstoned", "id", id, "at", pollStartedAt)
	}

	if err := b.flagDependencyArchived(ctx); err != nil {
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

// flagDependencyArchived walks children whose depends_on_features
// references a tombstoned (archived) sibling. Each such child gets
// cascade-archived + a WARN log per spec §6 rubric A.
func (b *BriefLoader) flagDependencyArchived(ctx context.Context) error {
	rows, err := b.db.SQL().QueryContext(ctx, `
		SELECT id, depends_on_features FROM work_items
		WHERE source = ? AND status != ?`,
		string(state.SourceBrief), string(state.WorkStatusArchived))
	if err != nil {
		return fmt.Errorf("brief_loader: scan deps: %w", err)
	}
	type pending struct {
		id   string
		deps []string
	}
	var rowsList []pending
	for rows.Next() {
		var id, depsJSON string
		if err := rows.Scan(&id, &depsJSON); err != nil {
			rows.Close()
			return err
		}
		var deps []string
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			rows.Close()
			return err
		}
		rowsList = append(rowsList, pending{id, deps})
	}
	rows.Close()

	for _, r := range rowsList {
		for _, dep := range r.deps {
			depItem, err := b.db.GetWorkItem(ctx, dep)
			if err != nil {
				if errors.Is(err, state.ErrWorkItemNotFound) {
					continue
				}
				return err
			}
			if depItem.Status == state.WorkStatusArchived {
				slog.Warn("child.dependency_archived", "child", r.id, "dep", dep)
				if _, err := b.db.SQL().ExecContext(ctx,
					`UPDATE work_items SET status = ?, updated_at = ? WHERE id = ?`,
					string(state.WorkStatusArchived), b.now().UTC().Unix(), r.id); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

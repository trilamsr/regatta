// Package state — work_items types and method signatures.
//
// Universal-queue spec §2.2. AdapterSync (source=adapter) and
// BriefLoader (source=brief) both upsert here. Scheduler reads via
// the join-based ListSpawnable and reserves directly into agents in
// one transaction. Cascade-soft: archived parents do not kill
// in-flight child agents; the child row's acceptance_json snapshot
// keeps validation self-contained.
//
// Methods split across:
//   - work_items_upsert.go: UpsertWorkItem, TombstoneBySource, CascadeArchiveChildren
//   - work_items_query.go (A4): ListSpawnable, CycleCheck, ListByParent
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// WorkItemKind enumerates work_items.kind values.
type WorkItemKind string

// Work item kinds per spec §2.2.
const (
	KindFeature WorkItemKind = "feature"
	KindProgram WorkItemKind = "program"
)

// WorkItemStatus enumerates work_items.status values.
type WorkItemStatus string

// Work item lifecycle statuses per spec §2.2.
const (
	WorkStatusPlanned  WorkItemStatus = "planned"
	WorkStatusRunning  WorkItemStatus = "running"
	WorkStatusPROpen   WorkItemStatus = "pr_open"
	WorkStatusMerged   WorkItemStatus = "merged"
	WorkStatusArchived WorkItemStatus = "archived"
	WorkStatusBlocked  WorkItemStatus = "blocked"
)

// WorkItemSource enumerates work_items.source values.
type WorkItemSource string

// Work item sources: AdapterSync writes SourceAdapter, BriefLoader
// writes SourceBrief. Used by TombstoneBySource to scope sweeps so
// the two producers cannot stomp each other's rows.
const (
	SourceAdapter WorkItemSource = "adapter"
	SourceBrief   WorkItemSource = "brief"
)

// WorkItem mirrors a row in work_items. depends_on_features and
// acceptance_json are stored as JSON text in sqlite; the Go fields
// here are the decoded slices.
type WorkItem struct {
	ID                string
	Kind              WorkItemKind
	Title             string
	Lane              string
	Status            WorkItemStatus
	ParentProgramID   string
	DependsOnFeatures []string
	AcceptanceJSON    string
	Source            WorkItemSource
	LastSeenAt        time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ErrWorkItemNotFound is returned by GetWorkItem when id does not
// exist. Distinct from sql.ErrNoRows so callers can branch on the
// typed sentinel.
var ErrWorkItemNotFound = errors.New("state: work_item not found")

// GetWorkItem fetches one row by id. Returns ErrWorkItemNotFound
// when the row does not exist.
func (d *DB) GetWorkItem(ctx context.Context, id string) (WorkItem, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT id, kind, title, lane, status,
		       COALESCE(parent_program_id, ''), depends_on_features,
		       acceptance_json, source, last_seen_at, created_at, updated_at
		FROM work_items WHERE id = ?`, id)
	var w WorkItem
	var depsJSON string
	var lastSeen, created, updated int64
	if err := row.Scan(&w.ID, &w.Kind, &w.Title, &w.Lane, &w.Status,
		&w.ParentProgramID, &depsJSON, &w.AcceptanceJSON, &w.Source,
		&lastSeen, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkItem{}, ErrWorkItemNotFound
		}
		return WorkItem{}, fmt.Errorf("state: scan work_item: %w", err)
	}
	if err := json.Unmarshal([]byte(depsJSON), &w.DependsOnFeatures); err != nil {
		return WorkItem{}, fmt.Errorf("state: decode deps for %s: %w", id, err)
	}
	w.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	w.CreatedAt = time.Unix(created, 0).UTC()
	w.UpdatedAt = time.Unix(updated, 0).UTC()
	return w, nil
}

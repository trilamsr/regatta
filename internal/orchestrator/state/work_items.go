// Package state — work_items types and method signatures.
//
// Universal-queue spec §2.2. AdapterSync (source=adapter) and
// BriefLoader (source=brief) both upsert here. Scheduler reads via
// the join-based ListSpawnable and reserves directly into agents in
// one transaction. Cascade-soft: archived parents do not kill
// in-flight child agents; the child row's acceptance_json snapshot
// keeps validation self-contained.
package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// WorkItemKind is the canonical schemas.WorkItemKind re-exported under
// the state package so callers that already depend on state need not
// also import schemas just to spell the kind enum.
type WorkItemKind = schemas.WorkItemKind

// Work item kinds per spec §2.2 — re-exported from contracts/schemas so
// state and schemas share one source of truth for the enum values.
const (
	KindFeature = schemas.KindFeature
	KindProgram = schemas.KindProgram
)

// WorkItemStatus enumerates work_items.status values.
type WorkItemStatus string

// Work item lifecycle statuses per spec §2.2. WorkStatusRejected is the
// terminal state for an approval-gate denial (spec §3.1 step 0.5); the
// scheduler reaches it via TransitionWorkItem.
const (
	WorkStatusPlanned  WorkItemStatus = "planned"
	WorkStatusRunning  WorkItemStatus = "running"
	WorkStatusPROpen   WorkItemStatus = "pr_open"
	WorkStatusMerged   WorkItemStatus = "merged"
	WorkStatusArchived WorkItemStatus = "archived"
	WorkStatusBlocked  WorkItemStatus = "blocked"
	WorkStatusRejected WorkItemStatus = "rejected"
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

// ErrInvalidWorkItemTransition is returned by TransitionWorkItem when
// the CAS predicate (status=from) does not match — either the row no
// longer exists or another writer already moved it past `from`. The
// CAS-loses-race posture is load-bearing: callers like the scheduler's
// approval gate MUST treat this as "another producer already settled
// the wi" rather than retry, so a terminal state cannot be resurrected.
var ErrInvalidWorkItemTransition = errors.New("state: invalid work_item transition")

// TransitionWorkItem flips work_items.status from→to atomically. The
// CAS predicate (status=from) ensures a concurrent writer that already
// moved the row past `from` wins; the caller observes
// ErrInvalidWorkItemTransition and treats the row as already-settled.
//
// Spec §3.1 step 0.5 names this transition for the approval-gate
// rejected path; the brief_loader cascade-archive path (archive a
// child whose dep is archived) uses the same primitive. Both call
// sites previously issued raw-SQL UPDATEs; consolidating here pins
// the CAS shape in one place so future state-package changes (e.g.
// an audit column or a transition matrix) flow to every transition.
//
// Unknown id and stale from collapse to one sentinel because the
// SQL CAS cannot distinguish them without a second round-trip, and
// callers branch on "did not transition" identically either way.
//
// TransitionWorkItem is the d.now() shim — production writers that
// thread a poll-time stamp (BriefLoader) MUST call TransitionWorkItemAt
// so the constructor-bound clock contract holds.
func (d *DB) TransitionWorkItem(ctx context.Context, id string, from, to WorkItemStatus) error {
	return d.TransitionWorkItemAt(ctx, id, from, to, d.now())
}

// TransitionWorkItemAt is the timestamp-explicit variant of
// TransitionWorkItem. See TransitionWorkItem for the contract; at is
// the poll-start tick threaded by BriefLoader-style callers so the
// constructor-bound clock cannot drift between sibling writes inside
// one Sync.
func (d *DB) TransitionWorkItemAt(ctx context.Context, id string, from, to WorkItemStatus, at time.Time) error {
	now := at.UTC().Unix()
	res, err := d.sql.ExecContext(ctx,
		`UPDATE work_items SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		string(to), now, id, string(from))
	if err != nil {
		return fmt.Errorf("state: transition work_item: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("state: transition work_item rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: id=%s from=%s to=%s", ErrInvalidWorkItemTransition, id, from, to)
	}
	return nil
}

// GetWorkItem fetches one row by id. Returns ErrWorkItemNotFound
// when the row does not exist.
func (d *DB) GetWorkItem(ctx context.Context, id string) (WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx, selectWorkItemsCols+` FROM work_items WHERE id = ?`, id)
	if err != nil {
		return WorkItem{}, fmt.Errorf("state: get work_item: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, err := scanWorkItems(rows)
	if err != nil {
		return WorkItem{}, err
	}
	if len(items) == 0 {
		return WorkItem{}, ErrWorkItemNotFound
	}
	return items[0], nil
}

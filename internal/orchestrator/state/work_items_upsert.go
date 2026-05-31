package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// UpsertWorkItem inserts a new work_items row or updates an existing
// one (matched by id). last_seen_at and updated_at are stamped from
// the caller-supplied at; created_at is preserved on update and set
// to at on insert. Production writers (AdapterSync, BriefLoader)
// pass their poll-start tick so concurrent producers never race on
// the DB's clock.
//
// per spec §2.2 — depends_on_features and acceptance_json are stored
// as JSON text. Empty slice -> "[]". AcceptanceJSON must be valid
// JSON; an empty string is normalized to "[]".
func (d *DB) UpsertWorkItem(ctx context.Context, item WorkItem, source WorkItemSource, at time.Time) error {
	depsJSON, err := encodeDeps(item.DependsOnFeatures)
	if err != nil {
		return fmt.Errorf("state: encode deps: %w", err)
	}
	accept := item.AcceptanceJSON
	if accept == "" {
		accept = "[]"
	}
	if !json.Valid([]byte(accept)) {
		return fmt.Errorf("state: acceptance_json for %s is not valid JSON", item.ID)
	}
	now := at.UTC().Unix()

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingCreated int64
	row := tx.QueryRowContext(ctx, `SELECT created_at FROM work_items WHERE id = ?`, item.ID)
	switch err := row.Scan(&existingCreated); {
	case err == nil:
		if _, err := tx.ExecContext(ctx, `
			UPDATE work_items SET
				kind = ?, title = ?, lane = ?, status = ?,
				parent_program_id = ?, depends_on_features = ?,
				acceptance_json = ?, source = ?, last_seen_at = ?,
				updated_at = ?
			WHERE id = ?`,
			string(item.Kind), item.Title, item.Lane, string(item.Status),
			nullable(item.ParentProgramID), depsJSON, accept,
			string(source), now, now, item.ID,
		); err != nil {
			return fmt.Errorf("state: update work_item: %w", err)
		}
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO work_items (
				id, kind, title, lane, status,
				parent_program_id, depends_on_features, acceptance_json,
				source, last_seen_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, string(item.Kind), item.Title, item.Lane, string(item.Status),
			nullable(item.ParentProgramID), depsJSON, accept,
			string(source), now, now, now,
		); err != nil {
			return fmt.Errorf("state: insert work_item: %w", err)
		}
	default:
		return fmt.Errorf("state: probe existing work_item: %w", err)
	}
	return tx.Commit()
}

// TombstoneBySource archives every row whose source matches and
// last_seen_at < before AND status is not already archived. Returns
// the list of archived IDs. Per-source so AdapterSync and BriefLoader
// cannot tombstone each other's rows. The caller-supplied before is
// used for both the cutoff and the updated_at stamp so a sweep is
// idempotent under retry.
func (d *DB) TombstoneBySource(ctx context.Context, source WorkItemSource, before time.Time) ([]string, error) {
	cutoff := before.UTC().Unix()
	rows, err := d.sql.QueryContext(ctx, `
		UPDATE work_items
		SET status = ?, updated_at = ?
		WHERE source = ? AND last_seen_at < ? AND status != ?
		RETURNING id`,
		string(WorkStatusArchived), cutoff, string(source), cutoff, string(WorkStatusArchived))
	if err != nil {
		return nil, fmt.Errorf("state: tombstone: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("state: scan tombstone id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CascadeArchiveChildren marks every live work_items row whose
// parent_program_id matches as archived and returns the IDs that
// were just flipped (rows already archived are skipped, both in
// the UPDATE and the return slice). Cascade-SOFT (spec §2.4): the
// agents table is not touched, so any in-flight agent continues to
// its natural terminal state. The caller-supplied at stamps
// updated_at so a sweep is idempotent under retry.
func (d *DB) CascadeArchiveChildren(ctx context.Context, parentID string, at time.Time) ([]string, error) {
	stamp := at.UTC().Unix()
	rows, err := d.sql.QueryContext(ctx, `
		UPDATE work_items SET status = ?, updated_at = ?
		WHERE parent_program_id = ? AND status != ?
		RETURNING id`,
		string(WorkStatusArchived), stamp, parentID, string(WorkStatusArchived))
	if err != nil {
		return nil, fmt.Errorf("state: cascade archive %s: %w", parentID, err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("state: scan cascade id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func encodeDeps(deps []string) (string, error) {
	if len(deps) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(deps)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

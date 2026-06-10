package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// UpsertWorkItem inserts or updates by id. Callers pass their
// poll-start tick for last_seen_at / updated_at so concurrent producers
// do not race on the DB clock; created_at is preserved on update.
// AcceptanceJSON must be valid JSON (empty string normalizes to "[]").
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

	return d.WithTx(ctx, func(tx *sql.Tx) error {
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
			var traceID string
			PersistTraceIDFromContext(ctx, &traceID)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO work_items (
					id, kind, title, lane, status,
					parent_program_id, depends_on_features, acceptance_json,
					source, last_seen_at, created_at, updated_at, trace_id
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				item.ID, string(item.Kind), item.Title, item.Lane, string(item.Status),
				nullable(item.ParentProgramID), depsJSON, accept,
				string(source), now, now, now, traceID,
			); err != nil {
				return fmt.Errorf("state: insert work_item: %w", err)
			}
		default:
			return fmt.Errorf("state: probe existing work_item: %w", err)
		}
		return nil
	})
}

// TombstoneBySource archives rows whose source matches and
// last_seen_at < before, then cascade-withdraws any agent bound to a
// tombstoned work_item that is still in pending or crashed. Per-source
// so AdapterSync and BriefLoader cannot tombstone each other's rows.
// Both flips commit in a single tx so a restart cannot observe an
// archived work_item paired with a still-pending agent (#1208 retro:
// orchestrator.recovered_crashed storm).
//
// Cascade-soft for in-flight states (spawning, running, pr_open,
// gates_running, awaiting_merge, gates_failed): those agents finish to
// their natural terminal — only pending and crashed (no live PID, no
// in-flight work) get withdrawn so the scheduler will not re-dispatch
// them on the next tick. Idempotent under retry.
func (d *DB) TombstoneBySource(ctx context.Context, source WorkItemSource, before time.Time) ([]string, error) {
	cutoff := before.UTC().Unix()
	var ids []string
	err := d.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			UPDATE work_items
			SET status = ?, updated_at = ?
			WHERE source = ? AND last_seen_at < ? AND status != ?
			RETURNING id`,
			string(WorkStatusArchived), cutoff, string(source), cutoff, string(WorkStatusArchived))
		if err != nil {
			return fmt.Errorf("state: tombstone: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return fmt.Errorf("state: scan tombstone id: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		// rows MUST be closed before further writes on the same tx;
		// sqlite serializes per-conn statements and a live cursor
		// blocks the cascade UPDATE below.
		if err := rows.Close(); err != nil {
			return fmt.Errorf("state: close tombstone rows: %w", err)
		}
		return cascadeWithdrawTombstonedAgentsTx(ctx, tx, ids, cutoff)
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// cascadeWithdrawTombstonedAgentsTx flips pending+crashed agents whose
// work_item_id is in tombstoned to AgentWithdrawn. Same tx as the
// work_items archive so the two flips commit atomically. Bypasses the
// per-agent FSM step by design: this is a bulk archive driven by the
// tombstone sweep, not a TransitionAgent flow. The crashed→withdrawn
// edge is in AgentEdges so per-agent paths stay correct too; the bulk
// UPDATE just avoids the N+1 cost of looping TransitionAgentTx.
func cascadeWithdrawTombstonedAgentsTx(ctx context.Context, tx *sql.Tx, tombstoned []string, cutoff int64) error {
	if len(tombstoned) == 0 {
		return nil
	}
	q := `UPDATE agents SET state = ?, updated_at = ?
	      WHERE state IN (?, ?) AND work_item_id IN (`
	args := make([]any, 0, 4+len(tombstoned))
	args = append(args, string(AgentWithdrawn), cutoff, string(AgentPending), string(AgentCrashed))
	for i, id := range tombstoned {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, id)
	}
	q += ")"
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("state: cascade-withdraw agents: %w", err)
	}
	return nil
}

// CascadeArchiveChildren archives live children of parentID and
// returns the flipped IDs. Cascade-SOFT (spec §2.4): in-flight agents
// run to their natural terminal state. Idempotent under retry.
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

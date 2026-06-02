package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ProcessedBrief is one row of the processed_briefs watermark table.
// Migration 0007. Issue #92 — restart-persistent brief replay defence.
type ProcessedBrief struct {
	ParentProgramID string
	LastProducedAt  time.Time
	BriefHMAC       string
	UpdatedAt       time.Time
}

// GetProcessedBrief returns the persisted watermark for parentID, or
// (zero, false, nil) when no brief has been accepted yet. Read-only —
// BriefLoader uses this as the durable replay-defence floor in
// preference to MaxUpdatedAtForBriefChildren (which is derived from
// mutable work_items and resets to zero when those rows are deleted).
func (d *DB) GetProcessedBrief(ctx context.Context, parentID string) (ProcessedBrief, bool, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT parent_program_id, last_produced_at, brief_hmac, updated_at
		FROM processed_briefs WHERE parent_program_id = ?`, parentID)
	var (
		pid, mac string
		prod, upd int64
	)
	if err := row.Scan(&pid, &prod, &mac, &upd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProcessedBrief{}, false, nil
		}
		return ProcessedBrief{}, false, fmt.Errorf("state: get processed_brief %s: %w", parentID, err)
	}
	return ProcessedBrief{
		ParentProgramID: pid,
		LastProducedAt:  time.Unix(prod, 0).UTC(),
		BriefHMAC:       mac,
		UpdatedAt:       time.Unix(upd, 0).UTC(),
	}, true, nil
}

// RecordProcessedBrief upserts the replay-defence watermark for
// parentID. Idempotent — repeated calls with the same payload are a
// no-op. Callers MUST have already verified the brief's signature and
// freshness; this is a write-side recorder, not a gate.
//
// at is the orchestrator's pollStartedAt; the function never reads the
// DB clock so test injection and audit clarity stay aligned with
// every other write in the BriefLoader path.
func (d *DB) RecordProcessedBrief(ctx context.Context, parentID string, producedAt time.Time, briefHMAC string, at time.Time) error {
	if parentID == "" {
		return fmt.Errorf("state: RecordProcessedBrief requires parent_program_id")
	}
	if briefHMAC == "" {
		return fmt.Errorf("state: RecordProcessedBrief requires brief_hmac")
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO processed_briefs(parent_program_id, last_produced_at, brief_hmac, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(parent_program_id) DO UPDATE SET
		  last_produced_at = excluded.last_produced_at,
		  brief_hmac       = excluded.brief_hmac,
		  updated_at       = excluded.updated_at`,
		parentID, producedAt.UTC().Unix(), briefHMAC, at.UTC().Unix())
	if err != nil {
		return fmt.Errorf("state: record processed_brief %s: %w", parentID, err)
	}
	return nil
}

// HasProcessedBriefHMAC reports whether briefHMAC has been recorded
// for any program. Used by BriefLoader to reject exact-replay even
// when ProducedAt happens to be > the watermark (clock skew across
// brief re-signs by the same planner pair).
func (d *DB) HasProcessedBriefHMAC(ctx context.Context, briefHMAC string) (bool, error) {
	if briefHMAC == "" {
		return false, nil
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT 1 FROM processed_briefs WHERE brief_hmac = ? LIMIT 1`, briefHMAC)
	var one int
	if err := row.Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("state: probe processed_brief hmac: %w", err)
	}
	return true, nil
}

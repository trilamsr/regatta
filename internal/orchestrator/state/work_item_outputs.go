package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
)

// ErrJournalNotFound is returned by GetLatestOutput / GetOutputByContentSHA
// when no row matches. Owned here (not in package orchestrator) because
// state is a leaf — orchestrator imports state, not the reverse — so the
// canonical sentinel must live downstream of the import edge. Mirrors the
// state-owned/orchestrator-re-exported pattern already used by ErrFlockHeld
// in package lockfile.
var ErrJournalNotFound = errors.New("orchestrator: outputs journal entry not found for work_item")

// OutputJournalEntry mirrors one row in work_item_outputs. OutputJSON
// is always the canonical form (sorted keys, no insignificant ws) so
// re-hashing entry.OutputJSON reproduces entry.ContentSHA byte-for-byte
// across orchestrator versions — replay-determinism contract from
// spec §4 and §6 A-rubric TestEdgeEval_Deterministic.
type OutputJournalEntry struct {
	ID         int64
	WorkItemID string
	AttemptNo  int
	ContentSHA string
	OutputJSON string
	ProducedAt time.Time
}

// AppendOutput canonicalises payload, computes sha256 over the canonical
// bytes, and inserts a row with the next attempt_no for workItemID. One
// SERIALIZABLE-equivalent transaction holds the MAX(attempt_no) read and
// the INSERT together so concurrent retries get distinct attempt numbers
// rather than UNIQUE(work_item_id, attempt_no) collisions (Open() caps
// the pool at one connection so writers serialize at the app layer).
//
// Spawner contract (spec §3.9): exactly one call per attempt, before the
// agent transitions to status='merged'. Returns the inserted row so the
// scheduler tick edge-eval hook can read ContentSHA without a follow-up
// query.
func (d *DB) AppendOutput(ctx context.Context, workItemID string, payload json.RawMessage) (OutputJournalEntry, error) {
	return d.AppendOutputAt(ctx, workItemID, payload, d.now())
}

// AppendOutputAt is the explicit-clock variant. New production writers
// (scheduler, spawner) should call this with the poll-start tick so two
// producers cannot race on the DB's clock — same constraint as
// UpsertWorkItemAt.
func (d *DB) AppendOutputAt(ctx context.Context, workItemID string, payload json.RawMessage, at time.Time) (OutputJournalEntry, error) {
	canonBytes, err := canon.CanonicaliseJSON([]byte(payload))
	if err != nil {
		return OutputJournalEntry{}, fmt.Errorf("state: canonicalise output for %s: %w", workItemID, err)
	}
	sum := sha256.Sum256(canonBytes)
	sha := hex.EncodeToString(sum[:])
	produced := at.UTC().Unix()

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return OutputJournalEntry{}, fmt.Errorf("state: begin journal tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var nextAttempt int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(attempt_no), 0) + 1
		 FROM work_item_outputs WHERE work_item_id = ?`, workItemID,
	).Scan(&nextAttempt); err != nil {
		return OutputJournalEntry{}, fmt.Errorf("state: next attempt_no for %s: %w", workItemID, err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO work_item_outputs
		 (work_item_id, attempt_no, content_sha, output_json, produced_at)
		 VALUES (?, ?, ?, ?, ?)`,
		workItemID, nextAttempt, sha, string(canonBytes), produced,
	)
	if err != nil {
		return OutputJournalEntry{}, fmt.Errorf("state: insert journal row for %s: %w", workItemID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return OutputJournalEntry{}, fmt.Errorf("state: journal last id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return OutputJournalEntry{}, fmt.Errorf("state: commit journal: %w", err)
	}
	return OutputJournalEntry{
		ID:         id,
		WorkItemID: workItemID,
		AttemptNo:  nextAttempt,
		ContentSHA: sha,
		OutputJSON: string(canonBytes),
		ProducedAt: time.Unix(produced, 0).UTC(),
	}, nil
}

// GetLatestOutput returns the highest-attempt_no row for workItemID, or
// wraps ErrJournalNotFound if no row exists. Predicate evaluation reads
// "latest only" per spec §3.8 re-load semantics so a freshly re-loaded
// brief routes against the new run's output and ignores stale attempts.
func (d *DB) GetLatestOutput(ctx context.Context, workItemID string) (OutputJournalEntry, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, work_item_id, attempt_no, content_sha, output_json, produced_at
		 FROM work_item_outputs
		 WHERE work_item_id = ?
		 ORDER BY attempt_no DESC LIMIT 1`, workItemID)
	return scanJournalRow(row, fmt.Sprintf("work_item_id=%s", workItemID))
}

// GetOutputByContentSHA returns the entry whose content_sha matches,
// preferring the lowest id when multiple rows share a sha (legal: two
// work_items can journal byte-identical payloads — spec §7 risk 11).
// Used by replay paths that need to seed evaluator inputs from a hash
// recorded on a fired edge.
func (d *DB) GetOutputByContentSHA(ctx context.Context, sha string) (OutputJournalEntry, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, work_item_id, attempt_no, content_sha, output_json, produced_at
		 FROM work_item_outputs WHERE content_sha = ? ORDER BY id LIMIT 1`, sha)
	return scanJournalRow(row, fmt.Sprintf("content_sha=%s", sha))
}

func scanJournalRow(row *sql.Row, scope string) (OutputJournalEntry, error) {
	var e OutputJournalEntry
	var produced int64
	if err := row.Scan(&e.ID, &e.WorkItemID, &e.AttemptNo,
		&e.ContentSHA, &e.OutputJSON, &produced); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OutputJournalEntry{}, fmt.Errorf("%w: %s", ErrJournalNotFound, scope)
		}
		return OutputJournalEntry{}, fmt.Errorf("state: scan journal row: %w", err)
	}
	e.ProducedAt = time.Unix(produced, 0).UTC()
	return e, nil
}

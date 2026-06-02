// Package substraterecovery is the explicit, audited carve-out from
// substrate's append-only invariant. The substrate package itself is
// UPDATE/DELETE-free (enforced by TestSubstrate_NoUpdateDeleteInSubstratePackage).
// Key rotation drills require rewriting the (sig_alg, sig_key_id,
// sig_mac) triple of existing rows so a retired key can be removed
// from the keyring without losing every pre-rotation event.
//
// Why a sibling package, not substrate itself?
//
//   1. The append-only lint stays a hard rule for the hot path. A row
//      reader cannot tell from substrate's surface that mutation is
//      possible — the API exposes AppendEvent + Fold + Verify, period.
//   2. Operators who grep for UPDATE substrate_events find the recovery
//      path immediately. The carve-out is named, not hidden.
//   3. The single mutation surface (UPDATE sig_* WHERE id) is small
//      enough to audit in one file. A row's signed payload is bit-
//      identical before and after; only the auth tag changes.
//
// Caller is `regatta keys recover` (cmd/regatta/keys.go) — the CLI is
// the only audited entry point. Spec:
// docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md §3.2 +
// §3.4.
package substraterecovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// ResignRow re-signs one substrate_events row identified by id. The
// row's existing signature MUST verify under verifyKeyring (a superset
// of the live keyring — operator-supplied during the recovery drill).
// The new signature uses newKey under newKeyID. Returns
// substrate.ErrUnverifiable if the current signature does not verify
// — no silent bypass.
func ResignRow(ctx context.Context, tx *sql.Tx, id string, verifyKeyring map[string][]byte, newKey []byte, newKeyID string) error {
	e, err := readRowForResign(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := substrate.Verify(e, verifyKeyring); err != nil {
		return fmt.Errorf("resign %s: %w", id, err)
	}
	if err := substrate.Sign(&e, newKey, newKeyID); err != nil {
		return fmt.Errorf("resign %s: %w", id, err)
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE substrate_events SET sig_alg = ?, sig_key_id = ?, sig_mac = ? WHERE id = ?`,
		e.SigAlg, e.SigKeyID, e.SigMAC, id,
	)
	if err != nil {
		return fmt.Errorf("resign %s: update: %w", id, err)
	}
	return nil
}

// ListRowsBySigKeyID returns the ids of every substrate_events row
// whose sig_key_id matches one of the supplied keyIDs. Operator-typed
// keyIDs bound the IN list; full scan is acceptable because recover
// is an interactive drill, not a hot path.
func ListRowsBySigKeyID(ctx context.Context, db *sql.DB, keyIDs []string) ([]string, error) {
	if len(keyIDs) == 0 {
		return nil, nil
	}
	placeholders := ""
	args := make([]any, 0, len(keyIDs))
	for i, kid := range keyIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, kid)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM substrate_events WHERE sig_key_id IN (`+placeholders+`) ORDER BY id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("substraterecovery: list-by-sig-key: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("substraterecovery: list-by-sig-key scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ForeignSigKeyIDs returns the distinct sig_key_id values present in
// substrate_events that are NOT in the live keyring. These are the
// rows recover must re-sign (or fail loudly if the operator did not
// supply the material via --extra-key).
func ForeignSigKeyIDs(ctx context.Context, db *sql.DB, live map[string][]byte) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT sig_key_id FROM substrate_events`)
	if err != nil {
		return nil, fmt.Errorf("substraterecovery: scan sig_key_ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var foreign []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("substraterecovery: scan sig_key_ids row: %w", err)
		}
		if _, ok := live[id]; !ok {
			foreign = append(foreign, id)
		}
	}
	return foreign, rows.Err()
}

// readRowForResign reads back the columns Sign needs to reconstruct
// the canonical payload, plus the existing signature for the verify
// step. Single-row PK lookup; lint-substrate-queries exempts id-only
// WHERE clauses.
func readRowForResign(ctx context.Context, tx *sql.Tx, id string) (substrate.Event, error) {
	var e substrate.Event
	var workItemID, supersedes sql.NullString
	var payload []byte
	var kindStr string
	row := tx.QueryRowContext(ctx,
		`SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
		        kind, key, payload_json, blob_digest, supersedes,
		        written_by, written_at, schema_version, nonce,
		        sig_alg, sig_key_id, sig_mac
		 FROM substrate_events WHERE id = ?`,
		id,
	)
	if err := row.Scan(
		&e.ID, &e.RunID, &workItemID, &e.TenantID, &e.TraceID, &e.SpanID,
		&kindStr, &e.Key, &payload, &e.BlobDigest, &supersedes,
		&e.WrittenBy, &e.WrittenAt, &e.SchemaVersion, &e.Nonce,
		&e.SigAlg, &e.SigKeyID, &e.SigMAC,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return substrate.Event{}, fmt.Errorf("substraterecovery: row %s not found", id)
		}
		return substrate.Event{}, fmt.Errorf("substraterecovery: scan: %w", err)
	}
	e.Kind = substrate.EventKind(kindStr)
	if workItemID.Valid {
		e.WorkItemID = workItemID.String
	}
	if supersedes.Valid {
		e.Supersedes = supersedes.String
	}
	if len(payload) > 0 {
		e.PayloadJSON = append(e.PayloadJSON[:0], payload...)
	}
	return e, nil
}

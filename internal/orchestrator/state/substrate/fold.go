package substrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Fold returns every event for (runID, kind) ordered by
// (written_at ASC, id ASC). Read happens under sqlite WAL snapshot
// isolation — appends never block reads.
//
// Ordering: spec §2.3 / §10 #16 pin (written_at, id) as the
// tiebreaker. ULID monotonicity gives a stable secondary sort even
// when two writers (impossible in Wave 1's single-writer model but
// load-bearing for the W9 multi-host story) emit the same ms.
//
// Callers needing tenant- or work-item-scoped folds use indexed
// helpers (FoldByWorkItem, FoldByTenant) — see lint-substrate-queries
// which rejects unscoped substrate_events SELECTs in production code.
func Fold(ctx context.Context, db *sql.DB, runID string, kind EventKind) ([]SubstrateEvent, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
		        kind, key, payload_json, blob_digest, supersedes,
		        written_by, written_at, schema_version, nonce,
		        sig_alg, sig_key_id, sig_mac
		 FROM substrate_events
		 WHERE run_id = ? AND kind = ?
		 ORDER BY written_at ASC, id ASC`,
		runID, string(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("substrate: fold query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SubstrateEvent
	for rows.Next() {
		var e SubstrateEvent
		var workItemID, supersedes sql.NullString
		var payload []byte
		var kindStr string
		if err := rows.Scan(
			&e.ID, &e.RunID, &workItemID, &e.TenantID, &e.TraceID, &e.SpanID,
			&kindStr, &e.Key, &payload, &e.BlobDigest, &supersedes,
			&e.WrittenBy, &e.WrittenAt, &e.SchemaVersion, &e.Nonce,
			&e.SigAlg, &e.SigKeyID, &e.SigMAC,
		); err != nil {
			return nil, fmt.Errorf("substrate: fold scan: %w", err)
		}
		e.Kind = EventKind(kindStr)
		if workItemID.Valid {
			e.WorkItemID = workItemID.String
		}
		if supersedes.Valid {
			e.Supersedes = supersedes.String
		}
		// Copy the payload bytes — sqlite reuses the row buffer between
		// Next() calls; aliasing would corrupt earlier events.
		if len(payload) > 0 {
			e.PayloadJSON = json.RawMessage(append([]byte(nil), payload...))
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("substrate: fold rows: %w", err)
	}
	return out, nil
}

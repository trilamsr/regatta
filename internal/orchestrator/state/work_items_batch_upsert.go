package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// batchUpsertChunk caps the rows-per-statement so the bound-parameter count
// stays under sqlite's SQLITE_MAX_VARIABLE_NUMBER (default 999). 13 columns
// × 64 rows = 832 params, leaving comfortable headroom even if a future
// migration adds a column.
const batchUpsertChunk = 64

// BatchUpsertWorkItems inserts or updates the given items in one
// transaction, using multi-row VALUES + ON CONFLICT(id) DO UPDATE so each
// chunk is a single sqlite round-trip. Functionally equivalent to calling
// UpsertWorkItem in a loop (same column-write semantics, created_at
// preserved on update via ON CONFLICT's untouched-column rule) but
// eliminates the per-row BeginTx + SELECT/INSERT/UPDATE probe overhead
// that AdapterSync + BriefLoader pay at MVP-2 scale (issue #89).
//
// All rows share `source` and `at` so the caller's poll-start tick remains
// the single clock — same constraint as UpsertWorkItem.
func (d *DB) BatchUpsertWorkItems(ctx context.Context, items []WorkItem, source WorkItemSource, at time.Time) error {
	if len(items) == 0 {
		return nil
	}
	now := at.UTC().Unix()
	var traceID string
	PersistTraceIDFromContext(ctx, &traceID)

	return d.WithTx(ctx, func(tx *sql.Tx) error {
		for start := 0; start < len(items); start += batchUpsertChunk {
			end := start + batchUpsertChunk
			if end > len(items) {
				end = len(items)
			}
			chunk := items[start:end]

			var sb strings.Builder
			sb.WriteString(`INSERT INTO work_items (
				id, kind, title, lane, status,
				parent_program_id, depends_on_features, acceptance_json,
				source, last_seen_at, created_at, updated_at, trace_id
			) VALUES `)
			args := make([]any, 0, len(chunk)*13)
			for i, item := range chunk {
				depsJSON, err := encodeDeps(item.DependsOnFeatures)
				if err != nil {
					return fmt.Errorf("state: encode deps for %s: %w", item.ID, err)
				}
				accept := item.AcceptanceJSON
				if accept == "" {
					accept = "[]"
				}
				if !json.Valid([]byte(accept)) {
					return fmt.Errorf("state: acceptance_json for %s is not valid JSON", item.ID)
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
				args = append(args,
					item.ID, string(item.Kind), item.Title, item.Lane, string(item.Status),
					nullable(item.ParentProgramID), depsJSON, accept,
					string(source), now, now, now, traceID,
				)
			}
			// ON CONFLICT(id) DO UPDATE refreshes the mutable columns; created_at
			// is intentionally absent so the original row's value is preserved.
			// trace_id likewise stays put — overwriting it would break the
			// forensic-join contract (spec §3.5: trace_id pins the trace that
			// _created_ the row).
			sb.WriteString(` ON CONFLICT(id) DO UPDATE SET
				kind = excluded.kind,
				title = excluded.title,
				lane = excluded.lane,
				status = excluded.status,
				parent_program_id = excluded.parent_program_id,
				depends_on_features = excluded.depends_on_features,
				acceptance_json = excluded.acceptance_json,
				source = excluded.source,
				last_seen_at = excluded.last_seen_at,
				updated_at = excluded.updated_at`)

			if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
				return fmt.Errorf("state: batch upsert work_items: %w", err)
			}
		}
		return nil
	})
}

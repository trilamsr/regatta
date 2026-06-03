package selfimprove

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SQLEventSource reads spec §4.1's event projection out of
// `substrate_events` (#659 + #665 landed the kind set + payload shape
// the rules consume). One outer query per scan unions all rule kinds
// (spec §9.1) — fingerprint bucketing happens in-memory.
//
// Read-only WAL connection pool per the W3 supervisor pattern: pass a
// *sql.DB opened with `?_journal_mode=WAL&mode=ro` so the writer never
// blocks on a scan.
type SQLEventSource struct {
	DB *sql.DB
}

// NewSQLEventSource wires the substrate reader; nil DB is a programmer
// error caught at Fetch time, not at construction — keeps the constructor
// no-fail so cmd/regatta wiring can build the detector before opening the DB.
func NewSQLEventSource(db *sql.DB) *SQLEventSource {
	return &SQLEventSource{DB: db}
}

// substrateRow is the payload shape spec §5.2 group_by fields map onto.
// json_extract returns nil for missing keys → empty string here, and
// rule groupBy closures already gate on non-empty so missing-payload
// rows fall out cleanly.
type substrateRow struct {
	GateKind        string   `json:"gate_kind"`
	GateReason      string   `json:"gate_reason"`
	BannedToken     string   `json:"banned_token"`
	ClaimText       string   `json:"claim_text"`
	FailureKind     string   `json:"failure_kind"`
	LeftoverPattern string   `json:"leftover_pattern"`
	AgentID         string   `json:"agent_id"`
	Tags            []string `json:"tags"`
}

// Fetch reads substrate_events rows since `since` whose kind is in
// `kinds`, projecting payload_json fields the spec §5.2 group_by
// closures consume. One SELECT per scan; the caller (Detector.Run)
// scatters the slice across rules in memory.
func (s *SQLEventSource) Fetch(ctx context.Context, since time.Time, kinds []string) ([]Event, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("selfimprove: SQLEventSource DB is nil")
	}
	if len(kinds) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(kinds))
	placeholders = placeholders[:len(placeholders)-1]
	// #nosec G201 — only the ?-placeholder count is interpolated; every
	// user-supplied kind flows through Query args.
	q := fmt.Sprintf(`SELECT id, kind, written_at, payload_json
	                  FROM substrate_events
	                  WHERE written_at >= ?
	                    AND kind IN (%s)
	                  ORDER BY written_at DESC`, placeholders)
	args := make([]any, 0, len(kinds)+1)
	args = append(args, since.UnixMilli())
	for _, k := range kinds {
		args = append(args, k)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("selfimprove: query substrate_events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Event
	for rows.Next() {
		var id, kind string
		var writtenAt int64
		var payload []byte
		if err := rows.Scan(&id, &kind, &writtenAt, &payload); err != nil {
			return nil, fmt.Errorf("selfimprove: scan: %w", err)
		}
		var row substrateRow
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &row); err != nil {
				// A malformed payload is a substrate-layer bug, not the
				// detector's concern — skip rather than abort the scan so
				// one bad row never blocks the rest.
				continue
			}
		}
		out = append(out, Event{
			ID:              id,
			OccurredAt:      time.UnixMilli(writtenAt).UTC(),
			Kind:            kind,
			GateKind:        row.GateKind,
			GateReason:      row.GateReason,
			BannedToken:     row.BannedToken,
			ClaimText:       row.ClaimText,
			FailureKind:     row.FailureKind,
			LeftoverPattern: row.LeftoverPattern,
			AgentID:         row.AgentID,
			Tags:            row.Tags,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("selfimprove: rows: %w", err)
	}
	return out, nil
}

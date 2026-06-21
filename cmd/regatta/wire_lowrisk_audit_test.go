package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge/lowrisk"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestLowRiskAuditSink_NilDBYieldsNilSink asserts a zero-deps caller (no db OR no HMAC key) yields a nil sink so the gate stays sink-less without panicking (MAY-270).
func TestLowRiskAuditSink_NilDBYieldsNilSink(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if sink := newLowRiskAuditSink(nil, []byte("k"), "k1", logger); sink != nil {
		t.Fatalf("nil db must yield nil sink; got non-nil")
	}
	db := openSchedulerTestDB(t)
	if sink := newLowRiskAuditSink(db, nil, "k1", logger); sink != nil {
		t.Fatalf("nil key must yield nil sink; got non-nil")
	}
	if sink := newLowRiskAuditSink(db, []byte("k"), "", logger); sink != nil {
		t.Fatalf("empty keyID must yield nil sink; got non-nil")
	}
}

// TestLowRiskAuditSink_EmitsRowOnHoldAndEligible asserts the sink writes one substrate_events row per decision (hold + eligible cases) under kind merge_low_risk_decision with the contracted payload shape (MAY-270 AC2 + AC3).
func TestLowRiskAuditSink_EmitsRowOnHoldAndEligible(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := openSchedulerTestDB(t)
	key := []byte("test-hmac-key-32-bytes-padded-xx")
	sink := newLowRiskAuditSink(db, key, "key1", logger)
	if sink == nil {
		t.Fatalf("sink nil despite deps wired")
	}

	ctx := context.Background()
	sink(ctx, lowrisk.AuditDecision{
		PRNumber: 101, Eligible: false, Reason: "load_bearing_path",
		LOCDelta: 5, SoakRemainingSec: 0,
	})
	sink(ctx, lowrisk.AuditDecision{
		PRNumber: 202, Eligible: true, Reason: "eligible",
		LOCDelta: 3, SoakRemainingSec: 0,
	})

	rows := readLowRiskRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("rows=%d; want 2", len(rows))
	}
	hold, pass := rows[0], rows[1]
	if hold.PR != 101 || hold.Eligible || hold.Reason != "load_bearing_path" || hold.LOCDelta != 5 {
		t.Errorf("hold row=%+v; want pr=101 eligible=false reason=load_bearing_path loc=5", hold)
	}
	if pass.PR != 202 || !pass.Eligible || pass.Reason != "eligible" || pass.LOCDelta != 3 {
		t.Errorf("pass row=%+v; want pr=202 eligible=true reason=eligible loc=3", pass)
	}
}

type lowRiskRow struct {
	PR               int    `json:"pr"`
	Eligible         bool   `json:"eligible"`
	Reason           string `json:"reason"`
	LOCDelta         int    `json:"loc_delta"`
	SoakRemainingSec int64  `json:"soak_remaining_sec"`
}

func readLowRiskRows(t *testing.T, db interface {
	WithTx(context.Context, func(*sql.Tx) error) error
}) []lowRiskRow {
	t.Helper()
	var rows []lowRiskRow
	err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
		q := `SELECT payload_json FROM substrate_events WHERE kind = ? ORDER BY written_at, id`
		r, err := tx.Query(q, string(substrate.KindMergeLowRiskDecision))
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		for r.Next() {
			var raw string
			if err := r.Scan(&raw); err != nil {
				return err
			}
			var row lowRiskRow
			if err := json.Unmarshal([]byte(raw), &row); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return r.Err()
	})
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	return rows
}

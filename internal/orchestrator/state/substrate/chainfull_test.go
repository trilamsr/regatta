package substrate_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestFullChainSweep_CleanChain_ReportsZeroBreaks pins BreakCount=0 + RowsScanned=N on a clean chain (#629).
func TestFullChainSweep_CleanChain_ReportsZeroBreaks(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const rows = 12
	for i := 0; i < rows; i++ {
		e := mkEvent(byte(i+1), "run-X", substrate.KindHeartbeat,
			`{"work_item_id":"WI-X","timestamp":1}`, now.Add(time.Duration(i)*time.Second))
		if err := appendEventTx(ctx, t, raw, e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Close primary writer so the ro reader gets a clean view.
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	ro, err := sql.Open("sqlite", substrate.ReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	ro.SetMaxOpenConns(1)
	rpt, err := substrate.SweepFullChain(ctx, substrate.FullChainConfig{
		RODB:            ro,
		Keyring:         testKeyring(),
		BatchSize:       5,
		InterBatchPause: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("SweepFullChain: %v", err)
	}
	if rpt.RowsScanned != rows {
		t.Fatalf("RowsScanned=%d want %d", rpt.RowsScanned, rows)
	}
	if rpt.BreakCount != 0 {
		t.Fatalf("BreakCount=%d want 0", rpt.BreakCount)
	}
	if rpt.FirstBreakEventID != "" {
		t.Fatalf("FirstBreakEventID=%q want empty", rpt.FirstBreakEventID)
	}
}

// TestFullChainSweep_DetectsOldBreak surfaces a tampered row older than the 24h Sweeper window (#629).
func TestFullChainSweep_DetectsOldBreak(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 5 clean rows, then 1 row whose payload is tampered post-sign.
	for i := 0; i < 5; i++ {
		e := mkEvent(byte(i+1), "run-Y", substrate.KindHeartbeat,
			`{"work_item_id":"WI-Y","timestamp":1}`, now.Add(time.Duration(i)*time.Second))
		if err := appendEventTx(ctx, t, raw, e); err != nil {
			t.Fatalf("append clean %d: %v", i, err)
		}
	}
	// Insert a tampered row directly via SQL to bypass AppendEvent's
	// sign step — emulates an external mutation old enough that the
	// 24h hot-window Sweeper would never re-touch it.
	tamperedID := substrate.Mint(now.Add(10 * time.Second))
	_, err = raw.ExecContext(ctx,
		`INSERT INTO substrate_events
		   (id, run_id, work_item_id, tenant_id, trace_id, span_id,
		    kind, key, payload_json, blob_digest, supersedes,
		    written_by, written_at, schema_version, nonce,
		    sig_alg, sig_key_id, sig_mac)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tamperedID, "run-Y", nil, substrate.DefaultTenantID, "", "",
		string(substrate.KindHeartbeat), "",
		`{"work_item_id":"WI-Y","timestamp":1,"tampered":true}`, "", nil,
		"tester", now.Add(10*time.Second).UnixMilli(), 1, "ff112233445566778899aabbccddeeff",
		"HMAC-SHA256", testKeyID, "0000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatalf("tampered insert: %v", err)
	}

	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	ro, err := sql.Open("sqlite", substrate.ReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	ro.SetMaxOpenConns(1)
	rpt, err := substrate.SweepFullChain(ctx, substrate.FullChainConfig{
		RODB:            ro,
		Keyring:         testKeyring(),
		BatchSize:       3,
		InterBatchPause: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("SweepFullChain: %v", err)
	}
	if rpt.RowsScanned != 6 {
		t.Fatalf("RowsScanned=%d want 6", rpt.RowsScanned)
	}
	if rpt.BreakCount != 1 {
		t.Fatalf("BreakCount=%d want 1 (tampered row)", rpt.BreakCount)
	}
	if rpt.FirstBreakEventID != tamperedID {
		t.Fatalf("FirstBreakEventID=%q want %q", rpt.FirstBreakEventID, tamperedID)
	}
}


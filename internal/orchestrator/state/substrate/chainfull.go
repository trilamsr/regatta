// Full-chain sweeper for issue #629 (OBS-B T2 deferred). The routine
// sweeper in chainbreak.go only re-verifies rows inside the trailing
// Window (default 24h), so a chain break older than 24h could survive
// indefinitely. The full-chain walker re-verifies every row head-to-tail
// regardless of age, and is intended to run weekly (or on-demand) from
// a separate scheduler / K8s CronJob so it never contends with the
// hot-path routine sweeper.
//
// Design choices (single-binary self-host context per repo CLAUDE.md):
//   - Same read-only WAL connection shape as Sweeper — no new pool.
//   - Bounded BatchSize + InterBatchPause so a long walk yields to the
//     writer between chunks. The full-chain pass is bytes-heavy (every
//     row touched), so the default InterBatchPause is intentionally
//     >Sweeper's to keep the writer-starvation window narrow.
//   - Returns a typed FullChainReport (break_count + first_break_at)
//     so the caller (cron / CLI / future audit endpoint) can fail-loud
//     when breaks exist without parsing log lines.

package substrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// FullChainConfig tunes the head-to-tail re-verifier. DBPath + Keyring
// are required; the rest fall back to defaults that prioritise writer
// yield over walk-throughput (this is the slow audit path, not the hot
// path — spec §4 default budgets do not apply).
type FullChainConfig struct {
	// DBPath is the absolute path to the substrate sqlite file.
	DBPath string

	// Keyring maps SigKeyID → HMAC key. Same shape Verify() consumes.
	Keyring map[string][]byte

	// BatchSize is the number of rows verified per chunk. Default 500
	// (intentionally smaller than Sweeper's 1000 — the full walk is
	// CPU-heavier per row because every row is verified, not just the
	// hot window).
	BatchSize int

	// InterBatchPause is the sleep between chunks. Default 100ms —
	// 2x Sweeper's 50ms because the full walk is long-running and
	// writer-fairness matters more than total wallclock.
	InterBatchPause time.Duration

	// Logger is the structured-log sink. Nil falls back to slog.Default.
	Logger *slog.Logger
}

// FullChainReport is the typed result of a full-chain walk. Callers
// inspect BreakCount to decide whether to alert / page / fail CI. The
// individual break events are emitted via the logger so the runbook
// stays grep-able; the report is the machine-readable summary.
type FullChainReport struct {
	// RowsScanned is the total row count touched by the walk.
	RowsScanned int

	// BreakCount is the count of rows whose HMAC verification failed
	// for a non-missing-key reason (real chain break).
	BreakCount int

	// MissingKeyCount is the count of rows whose SigKeyID was not in
	// the keyring at walk time. Not a chain break — surfaces the
	// operator's key-rotation lag (spec §10 R9).
	MissingKeyCount int

	// FirstBreakEventID is the substrate ULID of the oldest detected
	// break, or "" when BreakCount == 0. Newest-first walk order is
	// preserved so the operator's runbook can chase the most recent
	// break first; "first" here means first in encounter order.
	FirstBreakEventID string

	// StartedAt / FinishedAt bracket the wallclock of the walk so the
	// audit log can correlate against external events.
	StartedAt  time.Time
	FinishedAt time.Time
}

func (c FullChainConfig) withDefaults() FullChainConfig {
	if c.BatchSize == 0 {
		c.BatchSize = 500
	}
	if c.InterBatchPause == 0 {
		c.InterBatchPause = 100 * time.Millisecond
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// SweepFullChain walks every row in substrate_events head-to-tail and
// re-verifies HMAC signatures. Returns a FullChainReport summarising the
// walk; an error here means a sqlite/keyring infrastructure failure, not
// a chain break (breaks are reported via the BreakCount field).
//
// Context cancellation aborts the walk on the next chunk boundary; the
// returned report reflects the partial work done.
//
// Caller pattern (weekly CronJob):
//
//	rpt, err := substrate.SweepFullChain(ctx, cfg)
//	if err != nil { log.Fatal(err) }
//	if rpt.BreakCount > 0 { os.Exit(2) }
func SweepFullChain(ctx context.Context, cfg FullChainConfig) (FullChainReport, error) {
	if cfg.DBPath == "" {
		return FullChainReport{}, fmt.Errorf("substrate: full-chain sweep requires DBPath")
	}
	cfg = cfg.withDefaults()

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_query_only=1&mode=ro", cfg.DBPath)
	roDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return FullChainReport{}, fmt.Errorf("substrate: full-chain open ro: %w", err)
	}
	defer func() { _ = roDB.Close() }()
	roDB.SetMaxOpenConns(1)

	rpt := FullChainReport{StartedAt: time.Now().UTC()}
	defer func() { rpt.FinishedAt = time.Now().UTC() }()

	var lastID string
	for {
		if err := ctx.Err(); err != nil {
			return rpt, err
		}
		rows, err := fullChainFetchBatch(ctx, roDB, lastID, cfg.BatchSize)
		if err != nil {
			return rpt, fmt.Errorf("substrate: full-chain fetch: %w", err)
		}
		if len(rows) == 0 {
			return rpt, nil
		}
		for _, e := range rows {
			rpt.RowsScanned++
			verr := Verify(e, cfg.Keyring) //nolint:contextcheck // pure fn; ctx flows to logger
			if verr == nil {
				continue
			}
			if errIsMissingKey(verr) {
				rpt.MissingKeyCount++
				continue
			}
			rpt.BreakCount++
			if rpt.FirstBreakEventID == "" {
				rpt.FirstBreakEventID = e.ID
			}
			cfg.Logger.WarnContext(ctx, "substrate.full_chain.break_detected",
				slog.String("event_id", e.ID),
				slog.String("event_kind", string(e.Kind)),
				slog.Int64("written_at_ms", e.WrittenAt),
				slog.String("sig_key_id", e.SigKeyID))
		}
		lastID = rows[len(rows)-1].ID
		select {
		case <-ctx.Done():
			return rpt, ctx.Err()
		case <-time.After(cfg.InterBatchPause):
		}
	}
}

// fullChainFetchBatch reads the next chunk of substrate_events ordered
// by id DESC (newest first). Keyset pagination via id < afterID keeps
// each batch O(BatchSize). Unlike Sweeper.fetchBatch, there is no
// written_at cutoff — the walk covers the entire table.
func fullChainFetchBatch(ctx context.Context, ro *sql.DB, afterID string, batchSize int) ([]Event, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if afterID == "" {
		rows, err = ro.QueryContext(ctx,
			`SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
			        kind, key, payload_json, blob_digest, supersedes,
			        written_by, written_at, schema_version, nonce,
			        sig_alg, sig_key_id, sig_mac
			 FROM substrate_events
			 ORDER BY id DESC
			 LIMIT ?`, batchSize)
	} else {
		rows, err = ro.QueryContext(ctx,
			`SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
			        kind, key, payload_json, blob_digest, supersedes,
			        written_by, written_at, schema_version, nonce,
			        sig_alg, sig_key_id, sig_mac
			 FROM substrate_events
			 WHERE id < ?
			 ORDER BY id DESC
			 LIMIT ?`, afterID, batchSize)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var (
			e          Event
			wiID, sup  sql.NullString
			kindStr    string
			payloadStr string
		)
		if err := rows.Scan(&e.ID, &e.RunID, &wiID, &e.TenantID, &e.TraceID, &e.SpanID,
			&kindStr, &e.Key, &payloadStr, &e.BlobDigest, &sup,
			&e.WrittenBy, &e.WrittenAt, &e.SchemaVersion, &e.Nonce,
			&e.SigAlg, &e.SigKeyID, &e.SigMAC); err != nil {
			return nil, err
		}
		e.Kind = EventKind(kindStr)
		e.PayloadJSON = []byte(payloadStr)
		e.WorkItemID = wiID.String
		e.Supersedes = sup.String
		out = append(out, e)
	}
	return out, rows.Err()
}

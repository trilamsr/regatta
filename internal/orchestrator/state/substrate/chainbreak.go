// OBS Wave-B T2 background HMAC chain-break sweeper. Read-path Verify
// catches breaks the moment a reducer touches the row; this sweeper
// covers the "row never read again" case for the last-24h hot window.
// Self-host single-binary sqlite means one DB file; the sweeper opens
// a SECOND connection in read-only WAL mode so it never competes with
// the writer for the WAL lock.
//
// Why a separate connection: sqlite WAL mode allows concurrent readers
// AND one writer, but the writer holds the WAL header lock for ≤10 ms
// per append. A reader on the same *sql.DB pool occasionally races into
// that window. The PRAGMA query_only read-only pool guarantees this
// sweeper never blocks a writer, full stop.

package substrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// SweeperConfig tunes the background HMAC sweeper. Zero values pick
// the spec §4 defaults (1h interval, 24h window, 1000-row batch with
// 50ms inter-batch pause). Tests inject tighter values; prod takes
// defaults so the sweeper stays inside the < 10% throughput-degradation
// bench bound (BenchmarkSubstrate_AppendUnderSweeperLoad).
type SweeperConfig struct {
	// DBPath is the absolute path to the substrate sqlite file. The
	// sweeper opens its own read-only connection here so the writer's
	// pool sees zero new contention.
	DBPath string

	// Keyring resolves SigKeyID → HMAC key. Reuses the same shape
	// Verify() takes so callers do not maintain two keyring objects.
	Keyring map[string][]byte

	// Interval is the wakeup period between full sweeps. Spec default
	// 1h; tests pass 10ms-100ms to drive the loop deterministically.
	Interval time.Duration

	// Window is the trailing wall-time lookback. Spec default 24h;
	// rows older than (now - Window) are not re-verified by this
	// sweeper (the read-path emitter is the long-tail defense).
	Window time.Duration

	// BatchSize is the number of rows verified per chunk. Spec
	// default 1000; chunked so InterBatchPause yields to writers.
	BatchSize int

	// InterBatchPause is the sleep between chunks. Spec default 50ms;
	// shrinks the worst-case writer-starvation window from "one full
	// sweep" to "one chunk".
	InterBatchPause time.Duration

	// Logger is the structured-log sink. Nil falls back to
	// slog.Default() so embedded callers still get output.
	Logger *slog.Logger

	// Now is the wall-clock source for the window cutoff. Nil falls
	// back to time.Now; tests inject a fake clock so the window
	// boundary is deterministic.
	Now func() time.Time
}

// withDefaults fills zero fields per spec §4. Pure function so tests
// can assert the default contract against a fresh config.
func (c SweeperConfig) withDefaults() SweeperConfig {
	if c.Interval == 0 {
		c.Interval = time.Hour
	}
	if c.Window == 0 {
		c.Window = 24 * time.Hour
	}
	if c.BatchSize == 0 {
		c.BatchSize = 1000
	}
	if c.InterBatchPause == 0 {
		c.InterBatchPause = 50 * time.Millisecond
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Sweeper is the background chain-break detector goroutine. Start()
// returns immediately; the caller stores the Sweeper and calls
// Close() (or cancels the parent context) to stop the loop. Concurrent
// callers MUST NOT call Start twice on the same Sweeper — the goroutine
// owns the read-only connection and exits cleanly on context cancel.
type Sweeper struct {
	cfg    SweeperConfig
	roDB   *sql.DB
	done   chan struct{}
	cancel context.CancelFunc
}

// NewSweeper opens a read-only WAL connection to cfg.DBPath and returns
// a Sweeper ready for Start. The read-only pool sets PRAGMA query_only
// = ON so even a programming-error write from this connection returns
// an error rather than blocking the writer's WAL lock.
//
// Why _journal_mode=WAL even on read-only: sqlite drivers default to
// rollback-journal mode for fresh connections; explicit WAL keeps the
// reader on the same journal mode the writer uses, so readers see
// committed writes through the WAL frame rather than blocking on a
// rollback-journal lock.
func NewSweeper(cfg SweeperConfig) (*Sweeper, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("substrate: sweeper requires DBPath")
	}
	cfg = cfg.withDefaults()
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_query_only=1&mode=ro", cfg.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("substrate: sweeper open read-only DB: %w", err)
	}
	// Cap the pool at 1 so the sweeper's verify loop never opens a
	// second file descriptor against the same file (sqlite handles
	// concurrent readers, but one fd is enough for this workload and
	// shrinks the inode-cache footprint).
	db.SetMaxOpenConns(1)
	return &Sweeper{cfg: cfg, roDB: db, done: make(chan struct{})}, nil
}

// Start spawns the sweeper goroutine. Returns immediately; the loop
// runs until ctx is cancelled or Close() is called. The first sweep
// fires on the first Interval tick — NOT immediately — so a flap of
// rapid Start/Close in tests cannot pile up half-completed sweeps.
func (s *Sweeper) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.run(ctx)
}

// Close stops the sweeper and waits for the goroutine to exit. Idempotent
// — calling twice is a no-op so the substrate Close path can fan out
// to the sweeper without tracking its own "started" bool.
func (s *Sweeper) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	<-s.done
	if s.roDB != nil {
		return s.roDB.Close()
	}
	return nil
}

// run is the goroutine body. Interval-driven; one sweep per tick. The
// sweep walks rows from newest backward in BatchSize chunks; an
// InterBatchPause sleep between chunks yields to the writer. ctx
// cancellation aborts the in-flight sweep on the next chunk boundary.
func (s *Sweeper) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sweepOnce(ctx); err != nil && ctx.Err() == nil {
				// Log-and-continue: the sweeper is best-effort observability,
				// not a hard correctness gate. A transient sqlite error
				// (e.g. WAL checkpoint contention) must not kill the loop.
				s.cfg.Logger.WarnContext(ctx, "substrate.sweeper.error",
					slog.String("err", err.Error()))
			}
		}
	}
}

// SweepOnce runs a single sweep cycle synchronously. Exported for
// tests that want to drive the loop without waiting on the ticker.
// Returns nil on clean sweep, error on sqlite failure. Chain-break
// detections are recorded to the counter as a side effect — they do
// NOT bubble up as errors because they are operational signals, not
// programmer errors.
func (s *Sweeper) SweepOnce(ctx context.Context) error {
	return s.sweepOnce(ctx)
}

// sweepOnce walks substrate_events from newest backward, batched at
// cfg.BatchSize, until either the window cutoff is crossed or the
// table is exhausted. Each row's signature is re-verified; mismatches
// increment the chain-break counter AND emit a WARN log so the operator
// sees the break in the live tail.
//
// Window cutoff: rows older than (now - Window) are skipped. The cutoff
// is checked against written_at (the row's wall-time stamp), not id,
// because id is a ULID and the comparison would be lexicographic — fine
// in practice but written_at is the contract spec §4 documents.
func (s *Sweeper) sweepOnce(ctx context.Context) error {
	cutoff := s.cfg.Now().Add(-s.cfg.Window).UnixMilli()
	var lastID string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.fetchBatch(ctx, lastID, cutoff)
		if err != nil {
			return fmt.Errorf("substrate: sweeper fetch: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, e := range rows {
			if err := s.verifyRow(ctx, e); err != nil {
				// verifyRow logs + counter-bumps internally; the
				// returned error is for sqlite/keyring failures, not
				// chain-break detections.
				return err
			}
		}
		lastID = rows[len(rows)-1].ID
		// Inter-batch pause yields the WAL lock to writers. ctx.Done
		// short-circuits the sleep so Close() exits within InterBatchPause
		// rather than waiting on a fresh ticker.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cfg.InterBatchPause):
		}
	}
}

// fetchBatch reads the next chunk of events from the read-only pool.
// Ordered by id DESC so the sweeper walks newest-first and can short-
// circuit on the cutoff. The query uses keyset pagination on id (not
// LIMIT/OFFSET) so each batch is O(BatchSize) regardless of how far
// the sweep has progressed.
func (s *Sweeper) fetchBatch(ctx context.Context, afterID string, cutoff int64) ([]Event, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if afterID == "" {
		rows, err = s.roDB.QueryContext(ctx,
			`SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
			        kind, key, payload_json, blob_digest, supersedes,
			        written_by, written_at, schema_version, nonce,
			        sig_alg, sig_key_id, sig_mac
			 FROM substrate_events
			 WHERE written_at >= ?
			 ORDER BY id DESC
			 LIMIT ?`, cutoff, s.cfg.BatchSize)
	} else {
		rows, err = s.roDB.QueryContext(ctx,
			`SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
			        kind, key, payload_json, blob_digest, supersedes,
			        written_by, written_at, schema_version, nonce,
			        sig_alg, sig_key_id, sig_mac
			 FROM substrate_events
			 WHERE id < ? AND written_at >= ?
			 ORDER BY id DESC
			 LIMIT ?`, afterID, cutoff, s.cfg.BatchSize)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var (
			e           Event
			wiID, sup   sql.NullString
			kindStr     string
			payloadStr  string
		)
		// payload_json is TEXT in sqlite; the driver returns it as a
		// Go string. json.RawMessage scans cleanly from []byte but not
		// from string, so we route through a string intermediate.
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

// verifyRow re-runs the substrate Verify check against a single row.
// A real MAC mismatch increments the chain-break counter and emits a
// WARN log; missing-key errors are quiet (legitimate during key
// rotation per spec §10 R9). Other sqlite errors propagate so the
// sweeper logs them at the run() boundary.
//
// Verify is a pure HMAC compare and takes no ctx — the contextcheck
// linter false-positives on the pure-function call. The ctx parameter
// here flows to the counter emit + WARN log, which is the only place
// cancellation would matter.
func (s *Sweeper) verifyRow(ctx context.Context, e Event) error {
	err := Verify(e, s.cfg.Keyring) //nolint:contextcheck // pure fn; ctx flows to counter + log

	if err == nil {
		return nil
	}
	// Missing-key path is NOT a chain break (spec §10 R9 — operator
	// key rotation surfaces here legitimately).
	if errIsMissingKey(err) {
		return nil
	}
	// Real MAC mismatch: Verify() bumps the counter from its emit
	// site (one tally per row touched). The sweeper's role here is
	// the operator-visible WARN log with row context so the runbook
	// has a row_id + key_id to chase. Counter increment is upstream.
	s.cfg.Logger.WarnContext(ctx, "substrate.chain.break_detected",
		slog.String("event_id", e.ID),
		slog.String("event_kind", string(e.Kind)),
		slog.Int64("written_at_ms", e.WrittenAt),
		slog.String("sig_key_id", e.SigKeyID))
	return nil
}

// errIsMissingKey distinguishes the "unknown SigKeyID" path from the
// real-MAC-mismatch path. Verify returns ErrUnverifiable in both cases;
// we differentiate by inspecting the wrapped schemas.ErrUnverifiable
// fmt path. The substring check is bound to the Verify error-format
// string, but the alternative (typed sentinel for missing-key) requires
// a Verify signature change that is out of scope for Wave-B.
func errIsMissingKey(err error) bool {
	if err == nil {
		return false
	}
	// Verify wraps schemas.ErrUnverifiable for both cases; the unknown
	// key_id branch includes "unknown key_id" in the message while the
	// MAC compare returns the bare sentinel.
	msg := err.Error()
	return contains(msg, "unknown key_id")
}

// contains is a stdlib-free substring check kept inline so the sweeper
// file does not import strings just for one call. Identical semantics
// to strings.Contains; the inline form sheds one import line at no
// runtime cost.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// MinKeyLen re-exports the schemas constant so sweeper-test callers do
// not need to import contracts/schemas just to build a valid keyring.
const MinKeyLen = schemas.MinKeyLen

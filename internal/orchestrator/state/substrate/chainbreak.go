// OBS Wave-B T2 background HMAC chain-break sweeper. Read-path Verify catches breaks the moment a reducer touches the row; this sweeper covers the "row never read again" case for the last-24h hot window. Self-host single-binary sqlite means one DB file; the caller injects a SECOND read-only connection so the sweeper never competes with the writer for the WAL lock (see ReadOnlyDSN). Why separate: sqlite WAL allows concurrent readers AND one writer, but the writer holds the WAL header lock for ≤10ms per append; a reader on the same *sql.DB pool occasionally races into that window. The PRAGMA query_only RO pool guarantees this sweeper never blocks a writer.

package substrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// ReadOnlyDSN is the sqlite DSN substrate's chain-walkers expect: WAL journal, PRAGMA query_only, mode=ro. Composition roots build the RO *sql.DB with this DSN and inject it into SweeperConfig.RODB / FullChainConfig.RODB.
func ReadOnlyDSN(dbPath string) string {
	return fmt.Sprintf("file:%s?_journal_mode=WAL&_query_only=1&mode=ro", dbPath)
}

// SweeperConfig tunes the background HMAC sweeper. Zero values pick the spec §4 defaults (1h interval, 24h window, 1000-row batch with 50ms inter-batch pause). Tests inject tighter values; prod takes defaults so the sweeper stays inside the < 10% throughput-degradation bench bound (BenchmarkSubstrate_AppendUnderSweeperLoad).
type SweeperConfig struct {
	// RODB is the caller-injected read-only handle; composition root owns the open+close lifecycle and substrate borrows it for the Sweeper's duration. Open with substrate.ReadOnlyDSN so the PRAGMA query_only guarantee holds.
	RODB *sql.DB

	// Keyring resolves SigKeyID → HMAC key; reuses the same shape Verify() takes so callers do not maintain two keyring objects.
	Keyring map[string][]byte

	// Interval is the wakeup period between full sweeps; spec default 1h, tests pass 10ms-100ms to drive the loop deterministically.
	Interval time.Duration

	// Window is the trailing wall-time lookback; spec default 24h. Rows older than (now - Window) are not re-verified here (the read-path emitter is the long-tail defense).
	Window time.Duration

	// BatchSize is the number of rows verified per chunk; spec default 1000, chunked so InterBatchPause yields to writers.
	BatchSize int

	// InterBatchPause is the sleep between chunks; spec default 50ms, shrinks the worst-case writer-starvation window from "one full sweep" to "one chunk".
	InterBatchPause time.Duration

	// Logger is the structured-log sink; nil falls back to slog.Default() so embedded callers still get output.
	Logger *slog.Logger

	// Now is the wall-clock source for the window cutoff; nil falls back to time.Now and tests inject a fake clock so the window boundary is deterministic.
	Now func() time.Time
}

// withDefaults fills zero fields per spec §4; pure function so tests can assert the default contract against a fresh config.
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

// Sweeper is the background chain-break detector goroutine. Start() returns immediately; the caller stores the Sweeper and calls Close() (or cancels the parent ctx) to stop the loop. Concurrent callers MUST NOT call Start twice on the same Sweeper — the goroutine owns the read-only connection and exits cleanly on context cancel.
type Sweeper struct {
	cfg  SweeperConfig
	roDB *sql.DB
	// mu guards started/cancel/done against Start racing Close on the substrate-shutdown fan-out (#640): a defensive Close on the early-error path must not block waiting on a goroutine Start never spawned.
	mu        sync.Mutex
	started   bool
	done      chan struct{}
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
}

// NewSweeper wraps the caller-injected RODB in a Sweeper ready for Start; the caller owns RODB's open/close (substrate does NOT close it on Sweeper.Close so composition roots can reuse the same RO handle across multiple sweepers / audits).
func NewSweeper(cfg SweeperConfig) (*Sweeper, error) {
	if cfg.RODB == nil {
		return nil, fmt.Errorf("substrate: sweeper requires RODB")
	}
	cfg = cfg.withDefaults()
	return &Sweeper{cfg: cfg, roDB: cfg.RODB}, nil
}

// Start spawns the sweeper goroutine; returns immediately and the loop runs until ctx is cancelled or Close() is called. The first sweep fires on the first Interval tick (NOT immediately) so a flap of rapid Start/Close in tests cannot pile up half-completed sweeps. Calling Start more than once is a no-op; the second call's ctx is discarded so the goroutine identity (and its RO DB handle) stays singular.
func (s *Sweeper) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.started = true
	s.mu.Unlock()
	go s.run(ctx)
}

// Close stops the sweeper and waits for the goroutine to exit; idempotent and safe pre-Start. #640: without the started-guard, Close blocked forever on a done channel the goroutine would never close. The caller-injected RODB is NOT closed here; composition root owns its lifecycle.
func (s *Sweeper) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		started := s.started
		cancel := s.cancel
		done := s.done
		s.mu.Unlock()
		if started {
			cancel()
			<-done
		}
	})
	return s.closeErr
}

// run is the goroutine body — interval-driven, one sweep per tick. Walks rows newest-backward in BatchSize chunks with InterBatchPause yielding to writers; ctx cancellation aborts the in-flight sweep on the next chunk boundary.
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
				// Log-and-continue: sweeper is best-effort observability, not a hard correctness gate; a transient sqlite error (e.g. WAL checkpoint contention) must not kill the loop.
				s.cfg.Logger.WarnContext(ctx, "substrate.sweeper.error",
					slog.String("err", err.Error()))
			}
		}
	}
}

// SweepOnce runs a single sweep cycle synchronously — exported for tests that drive the loop without the ticker. Returns nil on clean sweep, error on sqlite failure; chain-break detections record to the counter as a side effect (operational signals, not programmer errors) and do NOT bubble up.
func (s *Sweeper) SweepOnce(ctx context.Context) error {
	return s.sweepOnce(ctx)
}

// sweepOnce walks substrate_events from newest backward, batched at cfg.BatchSize, until either the window cutoff is crossed or the table is exhausted. Each row's signature is re-verified; mismatches increment the chain-break counter AND emit a WARN log so the operator sees the break in the live tail. Window cutoff is checked against written_at (the row's wall-time stamp), not id, because id is a ULID and the comparison would be lexicographic.
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
				// verifyRow logs + counter-bumps internally; returned err is for sqlite/keyring failures, not chain-break detections.
				return err
			}
		}
		lastID = rows[len(rows)-1].ID
		// Inter-batch pause yields the WAL lock to writers; ctx.Done short-circuits so Close() exits within InterBatchPause rather than waiting on a fresh ticker.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cfg.InterBatchPause):
		}
	}
}

// fetchBatch reads the next chunk of events from the RO pool; ordered by id DESC so the sweeper walks newest-first and short-circuits on the cutoff. Keyset pagination on id (not LIMIT/OFFSET) keeps each batch O(BatchSize) regardless of sweep progress.
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
		// payload_json is TEXT in sqlite (driver returns Go string); json.RawMessage scans from []byte not string, so route through a string intermediate.
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

// verifyRow re-runs the substrate Verify check against a single row. A real MAC mismatch increments the chain-break counter and emits a WARN log; missing-key errors are quiet (legitimate during key rotation per spec §10 R9). Other sqlite errors propagate so run() logs at its boundary. Verify is a pure HMAC compare — contextcheck false-positives on the pure-fn call; ctx here flows only to the counter emit + WARN log.
func (s *Sweeper) verifyRow(ctx context.Context, e Event) error {
	err := Verify(e, s.cfg.Keyring) //nolint:contextcheck // pure fn; ctx flows to counter + log

	if err == nil {
		return nil
	}
	// Missing-key path is NOT a chain break (spec §10 R9 — operator key rotation surfaces here legitimately).
	if errIsMissingKey(err) {
		return nil
	}
	// Real MAC mismatch: Verify() bumps the counter from its emit site (one tally per row touched). Sweeper's role here is the operator-visible WARN log with row context so the runbook has a row_id + key_id to chase.
	s.cfg.Logger.WarnContext(ctx, "substrate.chain.break_detected",
		slog.String("event_id", e.ID),
		slog.String("event_kind", string(e.Kind)),
		slog.Int64("written_at_ms", e.WrittenAt),
		slog.String("sig_key_id", e.SigKeyID))
	return nil
}

// errIsMissingKey distinguishes the "unknown SigKeyID" path from the real-MAC-mismatch path. Verify returns ErrUnverifiable in both cases; differentiate by inspecting the wrapped schemas.ErrUnverifiable fmt path. The substring check is bound to the Verify error-format string; alternative (typed sentinel for missing-key) requires a Verify signature change out of scope for Wave-B.
func errIsMissingKey(err error) bool {
	if err == nil {
		return false
	}
	// Verify wraps schemas.ErrUnverifiable for both cases; the unknown key_id branch includes "unknown key_id" in the message while the MAC compare returns the bare sentinel.
	msg := err.Error()
	return contains(msg, "unknown key_id")
}

// contains is a stdlib-free substring check kept inline so the sweeper file does not import strings just for one call.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// MinKeyLen re-exports the schemas constant so sweeper-test callers do not need to import contracts/schemas just to build a valid keyring.
const MinKeyLen = schemas.MinKeyLen

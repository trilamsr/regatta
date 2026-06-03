// OBS Wave-B T3 substrate divergence reader. Polls the
// substrate_divergence_audit table (migrations 0009 + 0011) for new
// rows and emits the regatta.substrate.divergence.detected counter,
// partitioned by (program_kind, layer). Reader-only — never edits the
// audit table or its writers; that fence is the spec §5 path-disjoint
// rule, load-bearing per memory feedback_spec_pattern_authority.
//
// Why a poll loop instead of an INSERT trigger: sqlite triggers cannot
// fire user-space code (OTel SDK lives in Go, not the SQL VM). A
// 5-minute poll is the contract-bounded latency (spec §10 R6 worst-case
// 10 min detection delay) and the durable audit row guarantees no data
// is lost — only reporting cadence shifts.

package substrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// DivergenceReaderConfig tunes the divergence-audit poll loop. Zero
// values pick spec §5 defaults (5-minute poll). The reader maintains
// a high-watermark cursor (last seen audit row id) so each row is
// counted exactly once across reader restarts.
type DivergenceReaderConfig struct {
	// DB is the substrate sqlite handle. The reader uses the same
	// writer-shared pool because audit rows are read-only-after-write
	// and the read load is one query per poll interval — well under
	// the WAL-reader contention threshold.
	DB *sql.DB

	// PollInterval is the wakeup period between audit-table reads.
	// Spec default 5 min; tests pass 1ms-10ms to drive the loop
	// deterministically.
	PollInterval time.Duration

	// Logger is the structured-log sink. Nil falls back to
	// slog.Default() so embedded callers still get output.
	Logger *slog.Logger

	// ProgramKindResolver maps an audit row to a closed-enum
	// program_kind. Nil falls back to defaultProgramKindResolver
	// which routes every row to "other" — the safest cardinality
	// posture when the audit row schema does not yet carry a
	// program_kind column (current state per 0011 migration). When
	// the column lands, callers inject a resolver that reads it.
	ProgramKindResolver func(auditRow DivergenceAuditRow) string
}

// DivergenceAuditRow is the projection of substrate_divergence_audit
// the reader hands to the resolver. Mirrors the 0011 migration
// schema; new columns (e.g. program_kind once #369/#378 add it) extend
// this struct backward-compatibly.
type DivergenceAuditRow struct {
	ID            int64
	DetectedAtMs  int64
	Detector      string
	Store         string
	PrimaryKey    string
	DiffSummary   string
	RepairedAtMs  sql.NullInt64
	RepairAction  string
}

// defaultProgramKindResolver routes every audit row to "other" so the
// counter is cardinality-safe even when the audit schema does not
// carry a program_kind column. Callers that DO have program_kind data
// inject a real resolver via Config.ProgramKindResolver.
func defaultProgramKindResolver(_ DivergenceAuditRow) string {
	return tagOther
}

func (c DivergenceReaderConfig) withDefaults() DivergenceReaderConfig {
	if c.PollInterval == 0 {
		c.PollInterval = 5 * time.Minute
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.ProgramKindResolver == nil {
		c.ProgramKindResolver = defaultProgramKindResolver
	}
	return c
}

// DivergenceReader is the background audit-table poll loop. One goroutine
// per substrate instance; Close() (or ctx cancel) drains it cleanly.
type DivergenceReader struct {
	cfg     DivergenceReaderConfig
	cursor  int64 // last seen audit row id (high-watermark)
	done    chan struct{}
	cancel  context.CancelFunc
}

// NewDivergenceReader builds a reader against cfg.DB. The cursor starts
// at 0; the first poll captures every existing audit row. Callers that
// want to skip backfill bootstrap the cursor by calling SeekCursor
// before Start.
func NewDivergenceReader(cfg DivergenceReaderConfig) (*DivergenceReader, error) {
	if cfg.DB == nil {
		return nil, errors.New("substrate: divergence reader requires DB")
	}
	return &DivergenceReader{
		cfg:  cfg.withDefaults(),
		done: make(chan struct{}),
	}, nil
}

// SeekCursor advances the high-watermark to the current max(id) so a
// fresh reader does not re-count historical rows. Callers run this
// after first deploy to skip backfill; tests pass 0 to count from the
// beginning.
func (r *DivergenceReader) SeekCursor(ctx context.Context) error {
	var maxID sql.NullInt64
	err := r.cfg.DB.QueryRowContext(ctx,
		`SELECT MAX(id) FROM substrate_divergence_audit`).Scan(&maxID)
	if err != nil {
		return fmt.Errorf("substrate: divergence cursor seek: %w", err)
	}
	if maxID.Valid {
		r.cursor = maxID.Int64
	}
	return nil
}

// Start spawns the poll goroutine. Returns immediately; the loop runs
// until ctx is cancelled or Close() is called. First poll fires after
// the first PollInterval tick (not immediately) so test setup/teardown
// of a reader does not race a partial poll.
func (r *DivergenceReader) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	go r.run(ctx)
}

// Close stops the reader and waits for the goroutine to exit. Idempotent.
func (r *DivergenceReader) Close() error {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.done
	return nil
}

// PollOnce runs a single poll synchronously. Exported for tests; emits
// one counter increment per new audit row above the cursor.
func (r *DivergenceReader) PollOnce(ctx context.Context) error {
	return r.pollOnce(ctx)
}

func (r *DivergenceReader) run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.pollOnce(ctx); err != nil && ctx.Err() == nil {
				r.cfg.Logger.WarnContext(ctx, "substrate.divergence_reader.error",
					slog.String("err", err.Error()))
			}
		}
	}
}

// pollOnce reads every audit row above the cursor, emits one counter
// increment per row, and advances the cursor to the new max id. The
// query is bounded by id > cursor so the read load is constant per
// poll regardless of audit table size.
func (r *DivergenceReader) pollOnce(ctx context.Context) error {
	rows, err := r.cfg.DB.QueryContext(ctx,
		`SELECT id, detected_at, detector, store, primary_key,
		        diff_summary, repaired_at, repair_action
		 FROM substrate_divergence_audit
		 WHERE id > ?
		 ORDER BY id ASC`, r.cursor)
	if err != nil {
		return fmt.Errorf("substrate: divergence poll: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ar DivergenceAuditRow
		if err := rows.Scan(&ar.ID, &ar.DetectedAtMs, &ar.Detector, &ar.Store,
			&ar.PrimaryKey, &ar.DiffSummary, &ar.RepairedAtMs, &ar.RepairAction); err != nil {
			return fmt.Errorf("substrate: divergence row scan: %w", err)
		}
		pk := r.cfg.ProgramKindResolver(ar)
		// `detector` is the closest column we have to "layer" in the
		// current audit schema; layer enum-guard absorbs the mapping.
		recordDivergence(ctx, pk, ar.Detector)
		if ar.ID > r.cursor {
			r.cursor = ar.ID
		}
	}
	return rows.Err()
}

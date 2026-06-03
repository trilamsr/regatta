package history

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/trilamsr/regatta/internal/dbutil"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// substrateImpl is the substrate-default DurableHistory. Append
// thin-wraps substrate.AppendEvent inside a single-statement tx so
// callers do not have to manage substrate's tx contract themselves;
// Tail and Replay are Phase X stubs returning ErrUnsupported.
type substrateImpl struct {
	db    *sql.DB
	key   []byte
	keyID string
}

// NewSubstrate builds the substrate-default DurableHistory. The HMAC
// key + keyID inject at construction (no env reads inside impl) so
// tests and prod share one wiring shape.
func NewSubstrate(db *sql.DB, key []byte, keyID string) DurableHistory {
	return &substrateImpl{db: db, key: key, keyID: keyID}
}

// Append writes ev under runID through substrate.AppendEvent. The
// runID argument is the contract; ev.RunID must agree (defence in
// depth per ErrRunIDMismatch). A fresh tx wraps the single INSERT so
// substrate's existing validate/sign/cycle-check/insert pipeline
// runs in one atomic step.
func (s *substrateImpl) Append(ctx context.Context, runID string, ev substrate.Event) error {
	if ev.RunID != "" && ev.RunID != runID {
		return fmt.Errorf("%w: arg=%q ev.RunID=%q", ErrRunIDMismatch, runID, ev.RunID)
	}
	ev.RunID = runID

	return dbutil.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return substrate.AppendEvent(ctx, tx, ev, s.key, s.keyID)
	})
}

// Tail is Phase X (spec §1 OUT). v1 surfaces ErrUnsupported so callers
// fail loud rather than receive nil channels.
func (s *substrateImpl) Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error) {
	return nil, nil, ErrUnsupported
}

// Replay is Phase X (spec §1 OUT + §11 F1/F2/F3). Wave-2 of S2-T1
// lands the engine + diff harness (parent §7 T2).
//
// OBS Wave-B T4: even the stub records a histogram observation at
// the function boundary so the metric scope is wired before the
// Phase-X impl lands. Defer-time recording with time.Since(start)
// is the single timestamp source (spec §6 R5: no double-count with
// the W6 trace span; both consume the same duration value).
func (s *substrateImpl) Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error) {
	start := time.Now()
	defer func() {
		// Stub records "error" outcome; the Phase-X impl will pass
		// the real outcome (match / divergent / cancelled). ReplayOpts
		// will carry program_kind in the Phase-X plumbing; until then
		// the histogram routes through the closed-enum "other" tag so
		// cardinality stays bounded.
		substrate.RecordReplayDuration(ctx, "other", "error", time.Since(start).Seconds())
	}()
	return nil, nil, ErrUnsupported
}

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

// substrateImpl is the substrate-default DurableHistory. Append wraps substrate.AppendEvent in a tx so callers skip substrate's tx contract; Tail/Replay are Phase-X stubs returning ErrUnsupported.
type substrateImpl struct {
	db    *sql.DB
	key   []byte
	keyID string
}

// NewSubstrate builds the substrate-default DurableHistory; HMAC key + keyID inject at construction (no env reads inside impl) so tests and prod share one wiring shape.
func NewSubstrate(db *sql.DB, key []byte, keyID string) DurableHistory {
	return &substrateImpl{db: db, key: key, keyID: keyID}
}

// Append writes ev under runID through substrate.AppendEvent; ev.RunID must match the runID arg (defence-in-depth per ErrRunIDMismatch).
func (s *substrateImpl) Append(ctx context.Context, runID string, ev substrate.Event) error {
	if ev.RunID != "" && ev.RunID != runID {
		return fmt.Errorf("%w: arg=%q ev.RunID=%q", ErrRunIDMismatch, runID, ev.RunID)
	}
	ev.RunID = runID

	return dbutil.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return substrate.AppendEvent(ctx, tx, ev, s.key, s.keyID)
	})
}

// Tail is Phase X (spec §1 OUT); returns ErrUnsupported so callers fail loud rather than receive nil channels.
func (s *substrateImpl) Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error) {
	return nil, nil, ErrUnsupported
}

// Replay is Phase X (spec §1 OUT + §11 F1/F2/F3); stub records the histogram observation at the function boundary so the metric scope wires before the Phase-X impl lands.
func (s *substrateImpl) Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error) {
	start := time.Now()
	defer func() {
		substrate.RecordReplayDuration(ctx, "other", "error", time.Since(start).Seconds())
	}()
	return nil, nil, ErrUnsupported
}

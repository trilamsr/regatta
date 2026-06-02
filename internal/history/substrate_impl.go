package history

import (
	"context"
	"database/sql"
	"fmt"
	"io"

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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("history: begin tx: %w", err)
	}
	if err := substrate.AppendEvent(ctx, tx, ev, s.key, s.keyID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("history: commit: %w", err)
	}
	return nil
}

// Tail is Phase X (spec §1 OUT). v1 surfaces ErrUnsupported so callers
// fail loud rather than receive nil channels.
func (s *substrateImpl) Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error) {
	return nil, nil, ErrUnsupported
}

// Replay is Phase X (spec §1 OUT + §11 F1/F2/F3). Wave-2 of S2-T1
// lands the engine + diff harness (parent §7 T2).
func (s *substrateImpl) Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error) {
	return nil, nil, ErrUnsupported
}

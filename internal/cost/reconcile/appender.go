package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// SubstrateAppender is the production Appender: every Tick lands a
// kind=budget_reconciled row in substrate_events inside one tx
// (spec §3.5). HMAC-signed under the configured key; the tx wraps the
// single INSERT so a sqlite error rolls back without polluting audit.
type SubstrateAppender struct {
	db    *sql.DB
	key   []byte
	keyID string

	// nonceSeq is the per-process monotonic counter folded into nonce
	// so UNIQUE(run_id, written_by, nonce) holds even at 1-tick-per-ms.
	// Multi-host reconcilers are out of scope until W9.
	nonceSeq uint64
}

// SubstrateAppenderConfig is the cmd/regatta ↔ Appender seam. DB is
// shared with the orchestrator; the HMAC key is the brief keyring's
// active entry — reconciler owns no separate key surface.
type SubstrateAppenderConfig struct {
	DB    *sql.DB
	Key   []byte
	KeyID string
}

// NewSubstrateAppender wires the production Appender. Bad config
// (missing DB or HMAC key) surfaces at Append time so the operator
// sees the failure in the structured log rather than at startup parse.
func NewSubstrateAppender(cfg SubstrateAppenderConfig) *SubstrateAppender {
	return &SubstrateAppender{db: cfg.DB, key: cfg.Key, keyID: cfg.KeyID}
}

// runIDReconciler pins the audit-graph component every reconciler-
// emitted row lives in (decoupled from any user run).
const runIDReconciler = "cost-reconciler"

// writtenByReconciler tags every reconciler-emitted row so operators
// can grep substrate_events for "did the reconciler run?".
const writtenByReconciler = "cost-reconciler"

// Append opens a tx, builds an Event with the spec §3.5 shape, hands
// it to substrate.AppendEvent, commits on success.
//
// Nonce = `rec-<unix_nano>-<seq>` rather than a payload-derived hash:
// payload-hash nonces tie replay-protection to canonicalised JSON
// bytes, so a hostile writer flipping one whitespace-insensitive byte
// could ship a "different" row past UNIQUE. The per-process seq
// guarantees uniqueness even when two ad-hoc Ticks share the same
// wall-clock millisecond.
func (a *SubstrateAppender) Append(ctx context.Context, tenantID, kind string, payload json.RawMessage, writtenAt time.Time) error {
	if a.db == nil {
		return errors.New("reconcile.SubstrateAppender: DB nil — wiring missing")
	}
	if len(a.key) == 0 {
		return errors.New("reconcile.SubstrateAppender: HMAC key empty — REGATTA_HMAC_KEY unset?")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile.SubstrateAppender: begin tx: %w", err)
	}
	// sqlite returns ErrTxDone on a post-commit rollback — benign.
	defer func() { _ = tx.Rollback() }()

	at := writtenAt.UTC()
	a.nonceSeq++
	ev := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runIDReconciler,
		TenantID:      tenantID,
		Kind:          substrate.EventKind(kind),
		PayloadJSON:   payload,
		WrittenBy:     writtenByReconciler,
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         fmt.Sprintf("rec-%d-%d", at.UnixNano(), a.nonceSeq),
	}
	if err := substrate.AppendEvent(ctx, tx, ev, a.key, a.keyID); err != nil {
		return fmt.Errorf("reconcile.SubstrateAppender: append: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reconcile.SubstrateAppender: commit: %w", err)
	}
	return nil
}

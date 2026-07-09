package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/merge/lowrisk"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// lowRiskAuditRunID names the substrate run_id every decision row carries
// so the operator can `regatta audit list --run lowrisk-gate` to read the
// classify history.
const lowRiskAuditRunID = "lowrisk-gate"

// newLowRiskAuditSink returns a lowrisk.AuditSink that persists each
// classify decision as one substrate_events row of kind
// KindMergeLowRiskDecision (MAY-270 AC2). Best-effort: write failures
// log + return — audit MUST NOT block the gate decision.
//
// Returns nil when the HMAC key is absent (zero-key deployments fall
// back to slog-only retention, same pattern as program.BriefAuditConfig).
func newLowRiskAuditSink(db *state.DB, key []byte, keyID string, logger *slog.Logger) lowrisk.AuditSink {
	if db == nil || len(key) == 0 || keyID == "" {
		return nil
	}
	return func(ctx context.Context, d lowrisk.AuditDecision) {
		payload, err := json.Marshal(struct {
			PR               int    `json:"pr"`
			Eligible         bool   `json:"eligible"`
			Reason           string `json:"reason"`
			LOCDelta         int    `json:"loc_delta"`
			SoakRemainingSec int64  `json:"soak_remaining_sec"`
		}{
			PR:               d.PRNumber,
			Eligible:         d.Eligible,
			Reason:           d.Reason,
			LOCDelta:         d.LOCDelta,
			SoakRemainingSec: d.SoakRemainingSec,
		})
		if err != nil {
			logger.Warn("lowrisk.audit_marshal_failed",
				"pr", d.PRNumber, "err", err.Error())
			return
		}
		at := time.Now().UTC()
		ev := substrate.SubstrateEvent{
			ID:            substrate.Mint(at),
			RunID:         lowRiskAuditRunID,
			TenantID:      substrate.DefaultTenantID,
			Kind:          substrate.KindMergeLowRiskDecision,
			PayloadJSON:   payload,
			WrittenBy:     "lowrisk_gate",
			WrittenAt:     at.UnixMilli(),
			SchemaVersion: 1,
			Nonce:         substrate.Mint(at),
		}
		if err := db.WithTx(ctx, func(tx *sql.Tx) error {
			return substrate.AppendEvent(ctx, tx, ev, key, keyID)
		}); err != nil {
			logger.Warn("lowrisk.audit_write_failed",
				"pr", d.PRNumber, "err", err.Error())
		}
	}
}

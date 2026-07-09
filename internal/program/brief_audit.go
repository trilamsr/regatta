// Brief rejection audit sink (#80). Best-effort substrate write runs
// alongside slog.Warn at every rejection site so the record survives
// restart, log rollover, and the log-shipping gap. Reuses substrate's
// HMAC-chained events table (feedback_research_design_principles —
// avoids a parallel audit_log table). Write failures log + drop;
// audit must NOT block brief sync.

package program

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// reasonMaxLen caps reason in the substrate payload. substrate CHECK
// pins payload_json at 1024 bytes; reserving ~512 leaves room for the
// path field + JSON framing under any realistic path length.
const reasonMaxLen = 512

// BriefAuditConfig carries the substrate-sink deps for durable
// rejection records. Zero value disables — deployments without
// REGATTA_HMAC_KEY keep slog.Warn as the only retention surface.
// HMAC tamper-detection (#80 requirement) comes for free via
// substrate.AppendEvent's existing chain.
type BriefAuditConfig struct {
	Key      []byte
	KeyID    string
	TenantID string
	RunID    string
}

// enabled reports whether the audit sink should fire. Key + KeyID +
// RunID must all be set; TenantID falls back at the callsite so a
// partial config logs rather than silently drops.
func (c BriefAuditConfig) enabled() bool {
	return len(c.Key) > 0 && c.KeyID != "" && c.RunID != ""
}

// recordBriefRejection writes one brief_rejected row. Best-effort:
// write failures log + return so the loop continues — slog.Warn at
// the callsite already captured the rejection.
func (b *BriefLoader) recordBriefRejection(ctx context.Context, path, reason string) {
	if !b.audit.enabled() {
		return
	}
	tenant := b.audit.TenantID
	if tenant == "" {
		tenant = substrate.DefaultTenantID
	}
	if len(reason) > reasonMaxLen {
		reason = reason[:reasonMaxLen]
	}
	payload, err := json.Marshal(struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}{Path: path, Reason: reason})
	if err != nil {
		// Marshal failure is structurally impossible (string fields
		// only); log rather than panic on a path the audit chain
		// depends on.
		b.log.Warn("brief.audit_marshal_failed", "path", path, "err", err.Error())
		return
	}
	at := b.auditNow()
	ev := substrate.SubstrateEvent{
		ID:            substrate.Mint(at),
		RunID:         b.audit.RunID,
		TenantID:      tenant,
		Kind:          substrate.KindBriefRejected,
		PayloadJSON:   payload,
		WrittenBy:     "brief_loader",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		// Fresh nonce per Sync attempt — every re-read of a tampered
		// brief logs a NEW row; dedup-by-path would silence the
		// "tampering ongoing" signal the dashboard depends on.
		Nonce: substrate.Mint(at),
	}
	if err := b.db.WithTx(ctx, func(tx *sql.Tx) error {
		return substrate.AppendEvent(ctx, tx, ev, b.audit.Key, b.audit.KeyID)
	}); err != nil {
		b.log.Warn("brief.audit_write_failed",
			slog.String("path", path),
			slog.String("err", err.Error()))
	}
}

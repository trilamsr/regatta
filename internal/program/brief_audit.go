// Brief rejection audit sink (issue #80). Best-effort substrate write
// runs alongside the existing slog.Warn at every rejection site so the
// rejection record survives orchestrator restart, log rollover, and the
// log-shipping gap the issue body calls out. Reuses the substrate
// HMAC-chained events table per feedback_research_design_principles —
// avoids the parallel audit_log table the issue first proposed.
//
// Failure mode: substrate write failures are logged + dropped. A broken
// audit sink must NOT block brief sync; the existing slog warn already
// captures the rejection for an operator with journalctl access.

package program

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// reasonMaxLen caps the rejection reason in the substrate payload. The
// substrate CHECK pins payload_json at 1024 bytes; reserving ~512 for
// the reason leaves room for the path field + JSON framing under any
// realistic file path length.
const reasonMaxLen = 512

// BriefAuditConfig carries the substrate-sink deps the loader uses when
// emitting a durable rejection record. An unset (zero-value) Audit
// disables the substrate sink so deployments without REGATTA_HMAC_KEY
// keep working — the legacy slog.Warn stays the only retention surface
// until the operator configures a key.
//
// Spec rationale: issue #80 lists "HMAC chain signature so audit-log
// tampering is detectable" as a requirement. Reusing substrate's
// existing chain (via substrate.AppendEvent) lands that for free.
type BriefAuditConfig struct {
	Key      []byte
	KeyID    string
	TenantID string
	RunID    string
}

// enabled reports whether the audit sink should fire. Both Key and RunID
// must be set; TenantID falls back to substrate.DefaultTenantID at the
// callsite so a partially-configured BriefAuditConfig logs the
// misconfiguration to the operator rather than silently dropping rows.
func (c BriefAuditConfig) enabled() bool {
	return len(c.Key) > 0 && c.KeyID != "" && c.RunID != ""
}

// recordBriefRejection writes one substrate brief_rejected row for
// (path, reason). Best-effort: a write failure logs and returns so the
// loop continues to the next brief — the legacy slog.Warn at the
// callsite already captured the rejection.
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
		// Marshal failure is structurally impossible (both fields are
		// strings) but logging-with-cause beats panicking on a path the
		// audit chain depends on.
		b.log.Warn("brief.audit_marshal_failed", "path", path, "err", err.Error())
		return
	}
	at := b.auditNow()
	ev := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         b.audit.RunID,
		TenantID:      tenant,
		Kind:          substrate.KindBriefRejected,
		PayloadJSON:   payload,
		WrittenBy:     "brief_loader",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		// Nonce is a fresh ULID per Sync attempt — same monotonic source
		// as ID. Intent: every Sync that re-reads a tampered brief logs
		// a NEW audit row (frequency matters for the operator dashboard);
		// dedup-by-path would silence the "tampering ongoing" signal.
		Nonce: substrate.Mint(at),
	}
	tx, err := b.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		b.log.Warn("brief.audit_tx_failed", "path", path, "err", err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if err := substrate.AppendEvent(ctx, tx, ev, b.audit.Key, b.audit.KeyID); err != nil {
		b.log.Warn("brief.audit_append_failed",
			slog.String("path", path),
			slog.String("err", err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		b.log.Warn("brief.audit_commit_failed", "path", path, "err", err.Error())
	}
}

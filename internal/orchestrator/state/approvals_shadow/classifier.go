// Package approvals_shadow holds the pure-data config + classifier for state's approvals shadow-write / read-substrate seams (one-way: never imports state). See specs/2026-06-04-state-package-split-design.md §4.2 + §5.T5.
//revive:disable:exported
package approvals_shadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ApprovalEventFields struct {
	ApprovalID string `json:"approval_id"`
	Transition string `json:"transition"`
	Actor      string `json:"actor"`
	TokenJTI   string `json:"token_jti"`
}

func BuildShadowPayload(approvalID, transition, actor, tokenJTI string) (json.RawMessage, error) {
	p := map[string]any{
		"approval_id": approvalID,
		"transition":  transition,
		"actor":       actor,
	}
	if tokenJTI != "" {
		p["token_jti"] = tokenJTI
	}
	return json.Marshal(p)
}

// ShadowNonce derives the substrate UNIQUE(run_id, written_by, nonce) replay key from event identity; tokenJTI lives in payload, not the column, per spec §3.5.
// INVARIANT: order must match buildApprovalShadowPayload call-sites in
// approvals_shadow.go. Reordering args silently breaks replay determinism.
func ShadowNonce(approvalID, transition, tokenJTI string, ts time.Time) string {
	h := sha256.Sum256([]byte(approvalID + "|" + transition + "|" + tokenJTI + "|" + ts.UTC().Format("20060102T150405.000000000Z")))
	return hex.EncodeToString(h[:16])
}

// SanitizeWrittenBy enforces 0006_substrate.sql written_by CHECK class (NOT GLOB '*[^a-zA-Z0-9_:.-]*'); empty→SystemActor, cap 128.
func SanitizeWrittenBy(actor string) string {
	var b strings.Builder
	for _, r := range actor {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == ':', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return SystemActor
	}
	if len(out) > 128 {
		out = out[:128]
	}
	return out
}

// ParseSubstrateApprovalPayload decodes a shadow-write payload into primitive
// fields. The subpackage takes primitive params (not state.ApprovalEvent) to
// break the importer-cycle risk per spec §6 R1; this projection drops opaque
// Payload / event_id, which the caller re-maps.
func ParseSubstrateApprovalPayload(payload []byte) (ApprovalEventFields, error) {
	var f ApprovalEventFields
	if err := json.Unmarshal(payload, &f); err != nil {
		return ApprovalEventFields{}, fmt.Errorf("substrate: parse approval payload: %w", err)
	}
	return f, nil
}

func TruncateDiff(s string) string {
	if len(s) <= 512 {
		return s
	}
	return s[:512]
}

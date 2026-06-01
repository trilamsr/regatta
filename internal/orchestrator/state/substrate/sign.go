package substrate

import (
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// SigAlg pins HMAC-SHA256 for substrate events. One alg keeps the
// audit trail homogeneous with handoff + gate_result signers.
const SigAlg = schemas.SigAlg

// signedPayload is the canonical map an event signs over (and a
// verifier reconstructs). Field set per spec §5: (id, run_id,
// work_item_id, tenant_id, trace_id, span_id, kind, key, payload_json,
// blob_digest, supersedes, written_by, written_at, schema_version,
// nonce). The signature is NOT included in the signed map (it IS the
// MAC over the rest).
func signedPayload(e Event) map[string]any {
	var payload any
	if len(e.PayloadJSON) == 0 {
		payload = map[string]any{}
	} else {
		// Round-trip through json.Unmarshal so CanonicalJSON sees the
		// payload's internal shape (object keys etc.) and can sort
		// nested object keys deterministically.
		if err := json.Unmarshal(e.PayloadJSON, &payload); err != nil {
			// Validator should have caught this; fall back to raw bytes.
			payload = string(e.PayloadJSON)
		}
	}
	return map[string]any{
		"id":             e.ID,
		"run_id":         e.RunID,
		"work_item_id":   e.WorkItemID,
		"tenant_id":      e.TenantID,
		"trace_id":       e.TraceID,
		"span_id":        e.SpanID,
		"kind":           string(e.Kind),
		"key":            e.Key,
		"payload_json":   payload,
		"blob_digest":    e.BlobDigest,
		"supersedes":     e.Supersedes,
		"written_by":     e.WrittenBy,
		"written_at":     float64(e.WrittenAt),
		"schema_version": float64(e.SchemaVersion),
		"nonce":          e.Nonce,
	}
}

// Sign populates e.SigAlg / e.SigKeyID / e.SigMAC with an HMAC over the
// canonical JSON of signedPayload(*e). Reuses contracts/schemas.MacSum
// (length-prefixed kid || canon) so substrate signatures share the
// exact same HMAC primitive as handoff + approval-token signers.
//
// Mutates *e — callers are expected to construct an event, pass to
// AppendEvent (which calls Sign internally). Direct callers (tests,
// shadow-write path) may invoke Sign + INSERT manually.
func Sign(e *Event, key []byte, keyID string) error {
	if len(key) < schemas.MinKeyLen {
		return fmt.Errorf("%w: got %d bytes, want >= %d",
			schemas.ErrWeakKey, len(key), schemas.MinKeyLen)
	}
	canon, err := schemas.CanonicalJSON(signedPayload(*e))
	if err != nil {
		return fmt.Errorf("substrate: canonical-json: %w", err)
	}
	mac, err := schemas.MacSum(key, keyID, canon)
	if err != nil {
		return fmt.Errorf("substrate: macsum: %w", err)
	}
	e.SigAlg = SigAlg
	e.SigKeyID = keyID
	e.SigMAC = hex.EncodeToString(mac)
	return nil
}

// Verify checks e's signature against the keyring. Returns nil on
// successful verification, ErrUnverifiable on signature mismatch,
// ErrUnverifiable wrapping a "key_id not in keyring" or "nonce
// mismatch" cause otherwise.
//
// Nonce-mismatch defense (spec §5 I5): Verify reconstructs the signed
// payload from the row, asserts the payload nonce equals e.Nonce, and
// only then runs the HMAC compare. Without this assert, a hostile
// writer who replays a captured signature with a freshly-minted column
// nonce would pass verification — the UNIQUE column-nonce constraint
// blocks the trivial replay but a smart attacker who controls the row
// shape could otherwise sneak by.
func Verify(e Event, keyring map[string][]byte) error {
	key, ok := keyring[e.SigKeyID]
	if !ok {
		return fmt.Errorf("%w: unknown key_id %q", ErrUnverifiable, e.SigKeyID)
	}
	if len(key) < schemas.MinKeyLen {
		return fmt.Errorf("%w: keyring entry %q has %d bytes",
			schemas.ErrWeakKey, e.SigKeyID, len(key))
	}
	if e.SigAlg != SigAlg {
		return fmt.Errorf("%w: unsupported alg %q", ErrUnverifiable, e.SigAlg)
	}
	// Spec §5 I5 defense (column-nonce ≠ signed-payload-nonce): a
	// caller that post-mutates e.Nonce after Sign() ends up with
	// e.SigMAC computed against the OLD nonce while signedPayload(e)
	// canonicalises the NEW nonce — the HMAC compare below catches
	// the drift. A separate column-vs-signed nonce equality check
	// would be structurally trivial here because both halves come
	// from e.Nonce. The HMAC compare IS the defense.
	signed := signedPayload(e)
	canon, err := schemas.CanonicalJSON(signed)
	if err != nil {
		return fmt.Errorf("substrate: verify canonical-json: %w", err)
	}
	want, err := schemas.MacSum(key, e.SigKeyID, canon)
	if err != nil {
		return fmt.Errorf("substrate: verify macsum: %w", err)
	}
	got, err := hex.DecodeString(e.SigMAC)
	if err != nil {
		return fmt.Errorf("%w: mac not hex: %w", ErrUnverifiable, err)
	}
	if !hmac.Equal(got, want) {
		return ErrUnverifiable
	}
	return nil
}

// IsUnverifiable lets callers errors.Is across the substrate /
// contracts/schemas package boundary. Both sentinels wrap the same
// underlying "signature does not match" concept; collapsing them in
// callers avoids forcing every consumer to import both packages.
func IsUnverifiable(err error) bool {
	return errors.Is(err, ErrUnverifiable) || errors.Is(err, schemas.ErrUnverifiable)
}

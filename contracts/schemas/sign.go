package schemas

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/canon"
)

// HMAC signing for tamper-evident gate results and program handoffs.
//
// Same key, same alg (HMAC-SHA256) for every signed artifact in
// Regatta -- see docs/design.md §Threat Model §Tamper-evident audit.
// The program layer REUSES this key; it does not introduce a new
// keyring (recursive-attack-surface defense).
//
// Canonical serialization is JCS-flavored: object keys sorted, no
// whitespace, no trailing newlines, UTF-8. Sufficient for HMAC over
// JSON shapes Regatta controls end-to-end. Not a full RFC 8785
// implementation (no number canonicalization); revisit when a
// non-Regatta producer signs a payload Regatta will verify.

// ErrUnverifiable is returned when a signature does not match.
var ErrUnverifiable = errors.New("schemas: signature unverifiable")

const (
	// SigAlg is the signature algorithm pinned for every signed
	// artifact (handoff, program_brief, gate_result). One alg keeps
	// the audit trail homogeneous.
	SigAlg = "HMAC-SHA256"
	// SigKey is the JSON key for the signature subdocument inside
	// signed payloads. Stripping this key recovers the bytes that
	// were signed.
	SigKey = "signature"
	// MinKeyLen is the lower bound for HMAC-SHA256 keys. 32 bytes
	// matches the SHA-256 block size; below this, security degrades
	// from "computationally infeasible" to "depends on key randomness."
	MinKeyLen = 32
)

// ErrWeakKey distinguishes "short key" from "wrong key" so callers
// can fail-closed at startup rather than at first verify.
var ErrWeakKey = errors.New("schemas: hmac key shorter than MinKeyLen")

// ErrUnknownKeyID wraps ErrUnverifiable so existing errors.Is callers
// keep working; new code can match this sentinel for typed log events.
var ErrUnknownKeyID = fmt.Errorf("%w: unknown signing key_id", ErrUnverifiable)

// SignatureBlock is the structured signature subdocument carried by
// signed artifacts (handoff.schema.json, gate_result.schema.json).
type SignatureBlock struct {
	Alg   string `json:"alg"`    // SigAlg
	KeyID string `json:"key_id"` // operator-controlled label, e.g. "k1"
	MAC   string `json:"mac"`    // lowercase hex sha256 mac
}

// CanonicalJSON returns the canonical JSON encoding of v — a thin
// adapter over canon.Marshal so every signed/hashed byte in the system
// passes through the single canonicaliser in internal/canon (issue #553
// eliminated the schemas-local fork that produced different MACs from
// the canon path on large-int payloads).
func CanonicalJSON(v any) ([]byte, error) {
	return canon.Marshal(v)
}

// Sign returns a SignatureBlock for payload using key under keyID.
// The signature is computed over CanonicalJSON(payload) with the
// top-level "signature" field STRIPPED -- the only shape a verifier
// can reconstruct. Returns ErrWeakKey when len(key) < MinKeyLen.
func Sign(payload map[string]any, key []byte, keyID string) (SignatureBlock, error) {
	if len(key) < MinKeyLen {
		return SignatureBlock{}, fmt.Errorf("%w: got %d bytes, want >= %d", ErrWeakKey, len(key), MinKeyLen)
	}
	stripped := stripSignature(payload)
	canon, err := CanonicalJSON(stripped)
	if err != nil {
		return SignatureBlock{}, err
	}
	mac, err := MacSum(key, keyID, canon)
	if err != nil {
		return SignatureBlock{}, err
	}
	return SignatureBlock{
		Alg:   SigAlg,
		KeyID: keyID,
		MAC:   hex.EncodeToString(mac),
	}, nil
}

// Verify checks that payload['signature'] is valid HMAC of
// payload-sans-signature under the key associated with the
// signature's key_id in keyring. Returns ErrUnverifiable on
// mismatch, ErrUnknownKeyID if the key_id is not in keyring,
// ErrWeakKey if the resolved key fails the MinKeyLen check.
func Verify(payload map[string]any, keyring map[string][]byte) error {
	return VerifyWithAllowlist(payload, keyring, nil)
}

// VerifyWithAllowlist is Verify plus a key_id allowlist. When
// allowed is non-nil, the signature's key_id must appear in it OR
// verification fails with ErrUnknownKeyID before HMAC is even
// computed. Use when one process verifies signatures from multiple
// writers and you want to restrict which writers may sign which
// payload class.
func VerifyWithAllowlist(payload map[string]any, keyring map[string][]byte, allowed map[string]bool) error {
	sigRaw, ok := payload[SigKey]
	if !ok {
		return fmt.Errorf("%w: no signature field", ErrUnverifiable)
	}
	sigMap, ok := sigRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: signature is not an object", ErrUnverifiable)
	}
	alg, ok := sigMap["alg"].(string)
	if !ok {
		return fmt.Errorf("%w: alg field not a string", ErrUnverifiable)
	}
	if alg != SigAlg {
		return fmt.Errorf("%w: unsupported alg %q", ErrUnverifiable, alg)
	}
	keyID, ok := sigMap["key_id"].(string)
	if !ok {
		return fmt.Errorf("%w: key_id field not a string", ErrUnverifiable)
	}
	want, ok := sigMap["mac"].(string)
	if !ok {
		return fmt.Errorf("%w: mac field not a string", ErrUnverifiable)
	}
	if allowed != nil && !allowed[keyID] {
		return fmt.Errorf("%w: key_id %q not in allowlist", ErrUnknownKeyID, keyID)
	}
	key, ok := keyring[keyID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKeyID, keyID)
	}
	if len(key) < MinKeyLen {
		return fmt.Errorf("%w: keyring entry %q has %d bytes", ErrWeakKey, keyID, len(key))
	}
	stripped := stripSignature(payload)
	canon, err := CanonicalJSON(stripped)
	if err != nil {
		return err
	}
	mac, err := MacSum(key, keyID, canon)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnverifiable, err)
	}
	// LIVE-11: decode the wire token to raw bytes before constant-time
	// compare. hmac.Equal on hex ASCII leaks the longest-common-prefix
	// length via timing — convert both sides to raw before comparing so
	// the comparison runs over fixed-width MAC bytes.
	wantRaw, decErr := hex.DecodeString(want)
	if decErr != nil {
		return ErrUnverifiable
	}
	if !hmac.Equal(mac, wantRaw) {
		return ErrUnverifiable
	}
	return nil
}

// maxKeyIDLen caps keyID at uint32 range; a 4-GiB kid is never legitimate.
const maxKeyIDLen = 1 << 20

// MacSum binds (keyID, canonicalBody) into the HMAC input so that two
// keyring entries sharing identical key bytes under different kids
// cannot cross-verify. keyID is length-prefixed (uint32 BE) rather
// than NUL-separated because keyID is an unrestricted Go string and
// may legally contain a NUL byte.
//
// Exported so internal/canon (approval-token mint+verify) can share
// the exact same HMAC primitive instead of maintaining a duplicate.
func MacSum(key []byte, keyID string, canon []byte) ([]byte, error) {
	if len(keyID) > maxKeyIDLen {
		return nil, fmt.Errorf("schemas: keyID too long: %d bytes (max %d)", len(keyID), maxKeyIDLen)
	}
	h := hmac.New(sha256.New, key)
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(keyID))) // #nosec G115 — len(keyID) bounded above by maxKeyIDLen check.
	h.Write(lp[:])
	h.Write([]byte(keyID))
	h.Write(canon)
	return h.Sum(nil), nil
}

// stripSignature returns a shallow copy of payload without the
// top-level "signature" key. The returned map is independent of
// the input.
func stripSignature(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == SigKey {
			continue
		}
		out[k] = v
	}
	return out
}

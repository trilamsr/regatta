package schemas

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
)

// SignatureBlock is the structured signature subdocument carried by
// signed artifacts (handoff.schema.json, gate_result.schema.json).
//
// The string form `gate_result.go:GateResult.Signature` is kept for
// backward compat with v0 GateResults; new signed types use this
// struct directly.
type SignatureBlock struct {
	Alg   string `json:"alg"`    // SigAlg
	KeyID string `json:"key_id"` // operator-controlled label, e.g. "k1"
	MAC   string `json:"mac"`    // lowercase hex sha256 mac
}

// CanonicalJSON returns a deterministic byte form of v suitable for
// HMAC input. Object keys are recursively sorted; arrays preserve
// order; nil/empty handled.
//
// The function works by round-tripping through encoding/json into a
// generic map[string]any and re-emitting with sorted keys. This
// covers every Regatta-emitted JSON payload because none of them
// contain numbers requiring IEEE-754 normalization.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("schemas: marshal: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("schemas: round-trip unmarshal: %w", err)
	}
	return canonicalize(generic)
}

func canonicalize(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kj, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kj...)
			buf = append(buf, ':')
			child, err := canonicalize(t[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := []byte{'['}
		for i, x := range t {
			if i > 0 {
				buf = append(buf, ',')
			}
			child, err := canonicalize(x)
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		// Strings, numbers, booleans, nil -- encoding/json's default
		// emission is canonical enough for HMAC purposes.
		return json.Marshal(t)
	}
}

// Sign returns a SignatureBlock for payload using key under keyID.
// The signature is computed over CanonicalJSON(payload) with the
// "signature" field STRIPPED at the top level -- so the signature
// describes the payload-sans-signature, which is the only shape a
// verifier can reconstruct.
func Sign(payload map[string]any, key []byte, keyID string) (SignatureBlock, error) {
	stripped := stripSignature(payload)
	canon, err := CanonicalJSON(stripped)
	if err != nil {
		return SignatureBlock{}, err
	}
	h := hmac.New(sha256.New, key)
	h.Write(canon)
	return SignatureBlock{
		Alg:   SigAlg,
		KeyID: keyID,
		MAC:   hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// Verify checks that payload['signature'] is valid HMAC of
// payload-sans-signature under the key associated with the
// signature's key_id in keyring. Returns ErrUnverifiable on
// mismatch.
func Verify(payload map[string]any, keyring map[string][]byte) error {
	sigRaw, ok := payload[SigKey]
	if !ok {
		return fmt.Errorf("%w: no signature field", ErrUnverifiable)
	}
	sigMap, ok := sigRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: signature is not an object", ErrUnverifiable)
	}
	alg, _ := sigMap["alg"].(string)
	if alg != SigAlg {
		return fmt.Errorf("%w: unsupported alg %q", ErrUnverifiable, alg)
	}
	keyID, _ := sigMap["key_id"].(string)
	want, _ := sigMap["mac"].(string)
	key, ok := keyring[keyID]
	if !ok {
		return fmt.Errorf("%w: unknown key_id %q", ErrUnverifiable, keyID)
	}
	stripped := stripSignature(payload)
	canon, err := CanonicalJSON(stripped)
	if err != nil {
		return err
	}
	h := hmac.New(sha256.New, key)
	h.Write(canon)
	got := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(got), []byte(want)) {
		return ErrUnverifiable
	}
	return nil
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

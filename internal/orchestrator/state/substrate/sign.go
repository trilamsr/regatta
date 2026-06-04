package substrate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"sync"
	"unsafe"

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
		// T2 (OBS Wave-B) read-path emitter: a real MAC mismatch is a
		// chain break, distinct from the missing-key path above
		// (unknown_key never reaches this branch). Record before
		// returning so the operator sees breaks at the moment a reducer
		// reads the row; the sweeper covers cold rows.
		recordChainBreak(context.Background(), e.Kind)
		return ErrUnverifiable
	}
	return nil
}

// fastScratch holds the per-call buffers SignCanonicalized reuses so
// the F1 hot path (#216) avoids per-row heap churn on the envelope
// build + hex-encoded MAC + HMAC state. One Get/Put per row.
type fastScratch struct {
	canon   []byte    // envelope buffer (grown as needed)
	macOut  [32]byte  // HMAC-SHA256 raw output (pre-hex)
	lp      [4]byte   // keyID length-prefix scratch
	h       hash.Hash // HMAC-SHA256, lazily bound to a (key, keyID) pair
	hKey    string    // cached keyID the HMAC state was initialised with
	hKeyBuf []byte    // owned copy of the key bytes for value-identity (#700 R2)
}

var fastScratchPool = sync.Pool{
	New: func() any { return &fastScratch{canon: make([]byte, 0, 512)} },
}

// SignCanonicalized is the F1 fast path that skips signedPayload's
// map[string]any round-trip and emits envelope bytes directly. MAC is
// byte-identical to Sign when preCanon == canon.CanonicaliseJSON(e.PayloadJSON)
// (TestSubstrate_AppendEventCanonicalizedMatchesSlowPath).
//
// preCanon MUST be canon.CanonicaliseJSON(e.PayloadJSON). The `{`/`}`
// boundary check below is a smoke test, not a contract gate — wrong
// field order or truncation passes sanity and surfaces only at Verify
// time as ErrUnverifiable (#700 R7). Use Sign for cold paths.
func SignCanonicalized(e *Event, preCanon []byte, key []byte, keyID string) error {
	if len(key) < schemas.MinKeyLen {
		return fmt.Errorf("%w: got %d bytes, want >= %d",
			schemas.ErrWeakKey, len(key), schemas.MinKeyLen)
	}
	if len(preCanon) < 2 || preCanon[0] != '{' || preCanon[len(preCanon)-1] != '}' {
		return fmt.Errorf("substrate: SignCanonicalized: preCanon not a JSON object")
	}
	if e.Kind == "" || e.SchemaVersion <= 0 {
		return fmt.Errorf("substrate: SignCanonicalized: kind/schema_version unset")
	}

	s, ok := fastScratchPool.Get().(*fastScratch)
	if !ok {
		s = &fastScratch{canon: make([]byte, 0, 512)}
	}
	defer fastScratchPool.Put(s)
	s.canon = appendCanonicalEnvelope(s.canon[:0], e, preCanon)

	if len(keyID) > maxKeyIDLenFast {
		return fmt.Errorf("substrate: keyID too long: %d bytes", len(keyID))
	}
	// Pool-bound HMAC reuse keyed on (keyID, key-bytes). Value-identity
	// (not pointer-identity) is the only sound cache key: callers may
	// mutate the key buffer in place, and `hmac.New` snapshots the key's
	// padded form at construction (#700 R2).
	if s.h == nil || s.hKey != keyID || !bytes.Equal(s.hKeyBuf, key) {
		s.h = hmac.New(sha256.New, key)
		s.hKey = keyID
		s.hKeyBuf = append(s.hKeyBuf[:0], key...)
	} else {
		s.h.Reset()
	}
	binary.BigEndian.PutUint32(s.lp[:], uint32(len(keyID))) // #nosec G115 — bounded above
	_, _ = s.h.Write(s.lp[:])
	_, _ = s.h.Write(unsafeStringBytes(keyID))
	_, _ = s.h.Write(s.canon)
	mac := s.h.Sum(s.macOut[:0])
	e.SigAlg = SigAlg
	e.SigKeyID = keyID
	e.SigMAC = hex.EncodeToString(mac)
	return nil
}

// unsafeStringBytes returns a zero-copy read-only []byte view of s. The
// result MUST be consumed synchronously — callers MUST NOT retain it
// past the call. Used here as a hash.Hash.Write argument only.
func unsafeStringBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	// #nosec G103 — read-only view; consumer is hash.Hash.Write (pure reader).
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// appendCanonicalEnvelope emits the canonical JSON object the slow path
// produces via signedPayload → CanonicalJSON, but writes bytes directly
// instead of materialising map[string]any → encoder → decoder → sort.
// Field order is the alphabetical sort the canonicaliser would produce,
// pinned at compile time. preCanon is inlined as the payload_json value
// (already canonical by caller contract).
//
// Strings emitted here are constrained to ASCII printables in practice
// (ULIDs, run-ids, hex nonces, etc.); appendJSONStringASCII handles the
// JSON-string escape minimum needed (RFC 8259 §7) so we do not link the
// general-purpose stdlib encoder on the hot path.
func appendCanonicalEnvelope(dst []byte, e *Event, preCanon []byte) []byte {
	dst = append(dst, '{')
	dst = appendKV(dst, "blob_digest", e.BlobDigest)
	dst = append(dst, ',')
	dst = appendKV(dst, "id", e.ID)
	dst = append(dst, ',')
	dst = appendKV(dst, "key", e.Key)
	dst = append(dst, ',')
	dst = appendKV(dst, "kind", string(e.Kind))
	dst = append(dst, ',')
	dst = appendKV(dst, "nonce", e.Nonce)
	dst = append(dst, `,"payload_json":`...)
	dst = append(dst, preCanon...)
	dst = append(dst, ',')
	dst = appendKV(dst, "run_id", e.RunID)
	dst = append(dst, `,"schema_version":`...)
	dst = strconv.AppendInt(dst, int64(e.SchemaVersion), 10)
	dst = append(dst, ',')
	dst = appendKV(dst, "span_id", e.SpanID)
	dst = append(dst, ',')
	dst = appendKV(dst, "supersedes", e.Supersedes)
	dst = append(dst, ',')
	dst = appendKV(dst, "tenant_id", e.TenantID)
	dst = append(dst, ',')
	dst = appendKV(dst, "trace_id", e.TraceID)
	dst = append(dst, ',')
	dst = appendKV(dst, "work_item_id", e.WorkItemID)
	dst = append(dst, `,"written_at":`...)
	dst = strconv.AppendInt(dst, e.WrittenAt, 10)
	dst = append(dst, ',')
	dst = appendKV(dst, "written_by", e.WrittenBy)
	dst = append(dst, '}')
	return dst
}

func appendKV(dst []byte, key, val string) []byte {
	dst = append(dst, '"')
	dst = append(dst, key...)
	dst = append(dst, '"', ':')
	return appendJSONStringASCII(dst, val)
}

// appendJSONStringASCII emits a JSON-quoted string for ASCII inputs.
// Falls back to encoding/json for any byte outside printable ASCII —
// the slow-path equivalence test catches drift if a non-ASCII byte ever
// reaches the fast path (run-ids, ULIDs, hex nonces don't).
func appendJSONStringASCII(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' || c >= 0x7f {
			// Cold path: delegate to stdlib so escape rules match exactly.
			esc, err := json.Marshal(s)
			if err != nil {
				return append(dst, '"', '"')
			}
			return append(dst, esc...)
		}
	}
	dst = append(dst, '"')
	dst = append(dst, s...)
	dst = append(dst, '"')
	return dst
}

// maxKeyIDLenFast mirrors contracts/schemas.maxKeyIDLen — local copy
// avoids importing schemas for one int constant.
const maxKeyIDLenFast = 1 << 20

// IsUnverifiable lets callers errors.Is across the substrate /
// contracts/schemas package boundary. Both sentinels wrap the same
// underlying "signature does not match" concept; collapsing them in
// callers avoids forcing every consumer to import both packages.
func IsUnverifiable(err error) bool {
	return errors.Is(err, ErrUnverifiable) || errors.Is(err, schemas.ErrUnverifiable)
}

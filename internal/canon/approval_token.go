package canon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Approval-token mint + verify. Wire format (spec §5.2):
//   base64url_nopad(sig) "." base64url_nopad(canon-payload)
//
// The payload is wire-detached from its signature: cleaner shape for
// callback URLs than the schemas.Sign/Verify embedded-signature dance.
//
// HMAC primitive: byte-equivalent to contracts/schemas.macSum
// (length-prefixed key_id || canonical body, HMAC-SHA256). Reimplemented
// here rather than imported because macSum is package-private to
// contracts/schemas and approval-gate Wave-1 keeps file scope disjoint
// across parallel implementers. Drift between the two sites is guarded
// by an equivalence fixture test landed alongside the schemas seam in
// Wave 2.

// ErrTokenInvalid signals a malformed wire envelope: missing/extra `.`,
// non-base64url halves, or anything that fails before HMAC compare.
var ErrTokenInvalid = errors.New("canon: approval token malformed")

// ErrUnknownKeyID signals the token's kid is absent from the keyring.
var ErrUnknownKeyID = errors.New("canon: approval token unknown key_id")

// ErrUnverifiable signals HMAC mismatch, unknown-field rejection, or
// reviewer-identity mismatch — anything that means "we cannot trust
// this token" after framing/kid pass. Errors deliberately collapse to
// the same sentinel so a parser-oracle attacker cannot distinguish
// "wrong signature" from "wrong reviewer".
var ErrUnverifiable = errors.New("canon: approval token unverifiable")

// ErrTokenExpired signals now >= window. Distinct from ErrUnverifiable
// because operators need to retry/re-mint, not raise a security alert.
var ErrTokenExpired = errors.New("canon: approval token expired")

// kid-scan sentinels. extractKIDFromCanonical is the pre-HMAC oracle
// (spec §5.2 step 3); typed errors let callers map distinct framing
// failures to exit codes without string-matching. Verify wraps every
// kid-scan error with ErrTokenInvalid so framing failures continue to
// satisfy errors.Is(err, ErrTokenInvalid).
var (
	ErrKIDFraming      = errors.New("canon: not a JSON object")
	ErrKIDMissing      = errors.New("canon: kid key not found")
	ErrKIDInvalidUTF8  = errors.New("canon: kid not valid UTF-8")
	ErrKIDEscape       = errors.New("canon: kid contains escape sequence")
	ErrKIDControl      = errors.New("canon: kid contains control byte")
	ErrKIDUnterminated = errors.New("canon: unterminated kid string")
)

// TokenPayload is the canonical signed payload. JSON field order is
// canonicalised at mint-time (lex-sorted keys) — see CanonicaliseJSON.
type TokenPayload struct {
	KID      string `json:"kid"`
	WI       string `json:"wi"`
	AID      string `json:"aid"`
	Reviewer string `json:"reviewer"`
	JTI      string `json:"jti"`
	Window   int64  `json:"window"`
}

// Keyring resolves a key_id to its raw HMAC key bytes. Minimal seam so
// callers may back it with map[string][]byte (tests) or a fail-closed
// keystore (production wiring lands post-Wave-1).
type Keyring interface {
	Lookup(kid string) (key []byte, err error)
}

// MapKeyring is the trivial in-memory Keyring used by tests and by
// startup paths that load keys from a single config blob.
type MapKeyring map[string][]byte

// Lookup returns ErrUnknownKeyID when kid is absent.
func (m MapKeyring) Lookup(kid string) ([]byte, error) {
	k, ok := m[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, kid)
	}
	return k, nil
}

// MintToken builds a fresh approval token. The caller supplies the
// random source for jti; production callers pass crypto/rand.Reader.
// Returns the wire token and the raw jti string (for caller-side
// single-use bookkeeping).
func MintToken(kr Keyring, kid string, p TokenPayload, jtiRand io.Reader) (string, string, error) {
	key, err := kr.Lookup(kid)
	if err != nil {
		return "", "", err
	}
	var rb [16]byte
	if _, err := io.ReadFull(jtiRand, rb[:]); err != nil {
		return "", "", fmt.Errorf("canon: jti rand: %w", err)
	}
	jti := base64.RawURLEncoding.EncodeToString(rb[:])
	p.KID = kid
	p.JTI = jti
	body, err := canonicaliseTokenPayload(p)
	if err != nil {
		return "", "", err
	}
	mac := tokenMAC(key, kid, body)
	wire := base64.RawURLEncoding.EncodeToString(mac) + "." + base64.RawURLEncoding.EncodeToString(body)
	return wire, jti, nil
}

// VerifyToken authenticates a wire token end-to-end. Verification order
// is MANDATORY per spec §5.2: split → b64-decode → kid-scan → HMAC
// compare → json.Unmarshal(DisallowUnknownFields) → window+reviewer.
// HMAC is computed and compared BEFORE any json.Unmarshal of the body
// to deny parser-oracle attacks.
func VerifyToken(kr Keyring, wire string, expectReviewer string, now time.Time) (TokenPayload, error) {
	var zero TokenPayload
	dot := strings.IndexByte(wire, '.')
	if dot < 0 || dot == 0 || dot == len(wire)-1 {
		return zero, fmt.Errorf("%w: framing", ErrTokenInvalid)
	}
	if strings.IndexByte(wire[dot+1:], '.') >= 0 {
		return zero, fmt.Errorf("%w: framing", ErrTokenInvalid)
	}
	sig, err := base64.RawURLEncoding.DecodeString(wire[:dot])
	if err != nil {
		return zero, fmt.Errorf("%w: sig b64: %w", ErrTokenInvalid, err)
	}
	body, err := base64.RawURLEncoding.DecodeString(wire[dot+1:])
	if err != nil {
		return zero, fmt.Errorf("%w: payload b64: %w", ErrTokenInvalid, err)
	}
	kid, err := extractKIDFromCanonical(body)
	if err != nil {
		// Pre-HMAC kid scan failure: surface as malformed framing rather
		// than ErrUnverifiable so operator-facing logs distinguish
		// "garbage wire" from "wrong key".
		return zero, fmt.Errorf("%w: kid scan: %w", ErrTokenInvalid, err)
	}
	key, err := kr.Lookup(kid)
	if err != nil {
		return zero, err
	}
	computed := tokenMAC(key, kid, body)
	if !hmac.Equal(computed, sig) {
		return zero, ErrUnverifiable
	}
	// HMAC verified — only now decode the payload.
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	var p TokenPayload
	if err := dec.Decode(&p); err != nil {
		return zero, fmt.Errorf("%w: payload decode: %w", ErrUnverifiable, err)
	}
	if dec.More() {
		return zero, fmt.Errorf("%w: trailing bytes after payload", ErrUnverifiable)
	}
	if !now.Before(time.Unix(p.Window, 0)) {
		return zero, ErrTokenExpired
	}
	if p.Reviewer != expectReviewer {
		return zero, fmt.Errorf("%w: reviewer mismatch", ErrUnverifiable)
	}
	return p, nil
}

// canonicaliseTokenPayload marshals p with stdlib json, then runs the
// strict CanonicaliseJSON pass to guarantee lex-key-sorted output. We
// route through CanonicaliseJSON (not encoding/json directly) so the
// signed bytes match the format every other canon-consumer expects.
func canonicaliseTokenPayload(p TokenPayload) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return CanonicaliseJSON(raw)
}

// tokenMAC is byte-equivalent to contracts/schemas.macSum: the keyID is
// length-prefixed (uint32 BE) and fed into HMAC ahead of the body so
// that two keyring entries sharing identical key bytes under different
// kids cannot cross-verify.
const maxKeyIDLen = 1 << 20

func tokenMAC(key []byte, kid string, body []byte) []byte {
	// kid bound by maxKeyIDLen; longer kids cannot be minted because the
	// canonical prefix scan would already have failed framing.
	if len(kid) > maxKeyIDLen {
		// Caller-side invariant; return a zero MAC so hmac.Equal fails.
		return make([]byte, sha256.Size)
	}
	h := hmac.New(sha256.New, key)
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(kid))) // #nosec G115 — bounded by maxKeyIDLen.
	h.Write(lp[:])
	h.Write([]byte(kid))
	h.Write(body)
	return h.Sum(nil)
}

// extractKIDFromCanonical pulls the kid value from canonical-JSON body
// without a full json.Unmarshal: that is the post-HMAC step. canon
// sorts object keys lex (aid, jti, kid, reviewer, wi, window) so the
// "kid" prefix is unique inside the canonical envelope. The scanner is
// non-recursive and rejects escape sequences / control bytes to keep
// the pre-HMAC surface tight.
func extractKIDFromCanonical(body []byte) (string, error) {
	if len(body) < 2 || body[0] != '{' {
		return "", ErrKIDFraming
	}
	const needle = `"kid":"`
	idx := indexOf(body, []byte(needle))
	if idx < 0 {
		return "", ErrKIDMissing
	}
	start := idx + len(needle)
	// Parse JSON string literal until unescaped closing quote.
	var out []byte
	i := start
	for i < len(body) {
		c := body[i]
		if c == '"' {
			if !utf8.Valid(out) {
				return "", ErrKIDInvalidUTF8
			}
			return string(out), nil
		}
		if c == '\\' {
			// Reject escapes inside kid: a kid containing backslash
			// would not round-trip identically between schemas.macSum
			// and tokenMAC because kids are bytewise-fed. Operators
			// must use the alnum + _:.- charset (same as
			// approval_events.actor CHECK).
			return "", ErrKIDEscape
		}
		if c < 0x20 {
			return "", ErrKIDControl
		}
		out = append(out, c)
		i++
	}
	return "", ErrKIDUnterminated
}

// indexOf is bytes.Index without the bytes-package import — keeps the
// approval-token file imports minimal (no transitive surface area).
func indexOf(hay, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

package canon

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// testKey is a deterministic 32-byte HMAC key for round-trip tests.
var testKey = bytes.Repeat([]byte{0xAB}, 32)

// fixedReader yields a pinned byte sequence; used to assert jti derives
// from the injected reader rather than a global rand source.
type fixedReader struct {
	src []byte
	pos int
}

func (r *fixedReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	n := copy(p, r.src[r.pos:])
	r.pos += n
	return n, nil
}

func newTestKeyring() Keyring {
	return MapKeyring{"k1": testKey}
}

func mintForTest(t *testing.T, reviewer string, window int64) (wire, jti string) {
	t.Helper()
	kr := newTestKeyring()
	pinned := bytes.Repeat([]byte{0x42}, 16)
	wire, jti, err := MintToken(kr, "k1", TokenPayload{
		KID:      "k1",
		WI:       "wi-1",
		AID:      "aid-1",
		Reviewer: reviewer,
		Window:   window,
	}, &fixedReader{src: pinned})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return wire, jti
}

func TestApprovalToken_RoundTrip(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	window := time.Now().Add(time.Hour).Unix()
	wire, jti := mintForTest(t, "alice", window)
	got, err := VerifyToken(kr, wire, "alice", time.Now())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.KID != "k1" || got.WI != "wi-1" || got.AID != "aid-1" || got.Reviewer != "alice" {
		t.Fatalf("payload mismatch: %+v", got)
	}
	if got.JTI != jti {
		t.Fatalf("jti mismatch: got %q want %q", got.JTI, jti)
	}
	if got.Window != window {
		t.Fatalf("window mismatch: got %d want %d", got.Window, window)
	}
}

func TestApprovalToken_TamperedPayload(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	dot := strings.IndexByte(wire, '.')
	if dot < 0 {
		t.Fatal("no dot")
	}
	payB64 := wire[dot+1:]
	pay, err := base64.RawURLEncoding.DecodeString(payB64)
	if err != nil {
		t.Fatal(err)
	}
	// Flip the trailing byte (inside the window-int region) to keep the
	// "kid":"k1" prefix intact so the test exercises HMAC mismatch
	// rather than ErrUnknownKeyID.
	pay[len(pay)-2] ^= 0x01
	tampered := wire[:dot+1] + base64.RawURLEncoding.EncodeToString(pay)
	_, err = VerifyToken(kr, tampered, "alice", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable, got %v", err)
	}
}

func TestApprovalToken_TamperedSig(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	dot := strings.IndexByte(wire, '.')
	sig, err := base64.RawURLEncoding.DecodeString(wire[:dot])
	if err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(sig) + wire[dot:]
	_, err = VerifyToken(kr, tampered, "alice", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable, got %v", err)
	}
}

func TestApprovalToken_MalformedFraming(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	cases := []struct {
		name string
		wire string
	}{
		{"no_dot", "abcdef"},
		{"two_dots", "aa.bb.cc"},
		{"empty_sig", ".aGVsbG8"},
		{"empty_payload", "aGVsbG8."},
		{"both_empty", "."},
		{"non_b64_sig", "!!!.aGVsbG8"},
		{"non_b64_payload", "aGVsbG8.!!!"},
		{"empty_string", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := VerifyToken(kr, tc.wire, "alice", time.Now())
			if !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("want ErrTokenInvalid, got %v", err)
			}
		})
	}
}

func TestApprovalToken_UnknownKeyID(t *testing.T) {
	t.Parallel()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	emptyKR := MapKeyring{} // no k1
	_, err := VerifyToken(emptyKR, wire, "alice", time.Now())
	if !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("want ErrUnknownKeyID, got %v", err)
	}
}

func TestApprovalToken_Expired(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	past := time.Now().Add(-time.Hour).Unix()
	wire, _ := mintForTest(t, "alice", past)
	_, err := VerifyToken(kr, wire, "alice", time.Now())
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}

func TestApprovalToken_ReviewerMismatch(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	_, err := VerifyToken(kr, wire, "bob", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable, got %v", err)
	}
}

// craftSignedWire builds a wire token from arbitrary canonical-JSON
// payload bytes, signing with the same primitive Mint uses. It lets a
// test inject payloads Mint would never produce (extra fields, invalid
// JSON) while keeping the HMAC valid so the test exercises the
// post-HMAC path.
func craftSignedWire(t *testing.T, key []byte, kid string, payloadBytes []byte) string {
	t.Helper()
	mac, err := schemas.MacSum(key, kid, payloadBytes)
	if err != nil {
		t.Fatalf("macsum: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(mac) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes)
}

func TestApprovalToken_DisallowUnknownFields(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	window := time.Now().Add(time.Hour).Unix()
	// Canonical JSON for a token with an extra "foo" field, lex-key-sorted.
	// Order: aid, foo, jti, kid, reviewer, wi, window.
	jti := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 16))
	body := []byte(`{"aid":"aid-1","foo":"bar","jti":"` + jti + `","kid":"k1","reviewer":"alice","wi":"wi-1","window":` + itoa(window) + `}`)
	// Re-canonicalise to ensure byte-stable input.
	canon, err := CanonicaliseJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	wire := craftSignedWire(t, testKey, "k1", canon)
	_, err = VerifyToken(kr, wire, "alice", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable from unknown-field rejection, got %v", err)
	}
}

func TestJTI_UsesCryptoRand(t *testing.T) {
	t.Parallel()
	pinned := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	kr := newTestKeyring()
	_, jti, err := MintToken(kr, "k1", TokenPayload{
		KID: "k1", WI: "wi-1", AID: "aid-1", Reviewer: "alice", Window: time.Now().Add(time.Hour).Unix(),
	}, &fixedReader{src: pinned})
	if err != nil {
		t.Fatal(err)
	}
	got, err := base64.RawURLEncoding.DecodeString(jti)
	if err != nil {
		t.Fatalf("jti not base64url: %v", err)
	}
	if !bytes.Equal(got, pinned) {
		t.Fatalf("jti bytes = %x, want %x", got, pinned)
	}
}

func TestVerify_HMACBeforeJSONUnmarshal(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	// Payload has a scannable "kid":"k1" prefix (so kid-lookup succeeds)
	// but the post-kid bytes are NOT valid JSON. If verify did a full
	// json.Unmarshal before HMAC compare, this would surface as a JSON
	// parse error. With HMAC-first, the bogus payload fails MAC compare
	// → ErrUnverifiable. Parser-oracle defence (spec §5.2 step 4).
	bogus := []byte(`{"kid":"k1",NOT_JSON}`)
	wire := base64.RawURLEncoding.EncodeToString([]byte("bogus-sig-bytes")) +
		"." + base64.RawURLEncoding.EncodeToString(bogus)
	_, err := VerifyToken(kr, wire, "alice", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable (HMAC trips before JSON), got %v", err)
	}
}

// TestApprovalToken_FixedWireRegression pins the exact wire bytes MintToken emits for a fixed input (issue #131).
func TestApprovalToken_FixedWireRegression(t *testing.T) {
	t.Parallel()
	// Pinned: key=32×0xAB, kid=k1, jti=base64url(16×0x42), window=2000000000.
	const wantWire = "m342scR3QpsgbcN_aRvEJJ3u9fDOFurwfSPzdD1u34I.eyJhaWQiOiJhaWQtMSIsImp0aSI6IlFrSkNRa0pDUWtKQ1FrSkNRa0pDUWciLCJraWQiOiJrMSIsInJldmlld2VyIjoiYWxpY2UiLCJ3aSI6IndpLTEiLCJ3aW5kb3ciOjIwMDAwMDAwMDB9"
	kr := newTestKeyring()
	pinned := bytes.Repeat([]byte{0x42}, 16)
	gotWire, _, err := MintToken(kr, "k1", TokenPayload{
		KID: "k1", WI: "wi-1", AID: "aid-1", Reviewer: "alice", Window: 2000000000,
	}, &fixedReader{src: pinned})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if gotWire != wantWire {
		t.Fatalf("wire drift:\n got=%s\nwant=%s", gotWire, wantWire)
	}
	// Verify the pinned wire round-trips under the same key — guards against
	// a future change that breaks mint and verify in symmetric but incorrect ways.
	verifyAt := time.Unix(1999999999, 0)
	if _, err := VerifyToken(kr, wantWire, "alice", verifyAt); err != nil {
		t.Fatalf("verify pinned wire: %v", err)
	}
}

// TestVerifyToken_EmptyExpectReviewer_DerivesFromClaim covers Option A: passing "" skips the strict-compare and returns the claim's reviewer.
func TestVerifyToken_EmptyExpectReviewer_DerivesFromClaim(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	window := time.Now().Add(time.Hour).Unix()
	wire, _ := mintForTest(t, "alice", window)
	got, err := VerifyToken(kr, wire, "", time.Now())
	if err != nil {
		t.Fatalf("verify with empty expectReviewer: %v", err)
	}
	if got.Reviewer != "alice" {
		t.Fatalf("derived reviewer = %q want %q", got.Reviewer, "alice")
	}
}

// TestVerifyToken_EmptyExpectReviewer_TamperedSigStillFails proves HMAC verify happens BEFORE the derivation branch — passing "" cannot bypass forgery detection.
func TestVerifyToken_EmptyExpectReviewer_TamperedSigStillFails(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	dot := strings.IndexByte(wire, '.')
	sig, err := base64.RawURLEncoding.DecodeString(wire[:dot])
	if err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(sig) + wire[dot:]
	_, err = VerifyToken(kr, tampered, "", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable (HMAC trips before derivation), got %v", err)
	}
}

// TestVerifyToken_EmptyExpectReviewer_TamperedPayloadStillFails covers the body-flip path with empty expectReviewer.
func TestVerifyToken_EmptyExpectReviewer_TamperedPayloadStillFails(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	dot := strings.IndexByte(wire, '.')
	if dot < 0 {
		t.Fatal("no dot")
	}
	pay, err := base64.RawURLEncoding.DecodeString(wire[dot+1:])
	if err != nil {
		t.Fatal(err)
	}
	pay[len(pay)-2] ^= 0x01
	tampered := wire[:dot+1] + base64.RawURLEncoding.EncodeToString(pay)
	_, err = VerifyToken(kr, tampered, "", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable, got %v", err)
	}
}

// TestVerifyToken_EmptyExpectReviewer_ExpiredStillFails confirms window check still runs when expectReviewer is "".
func TestVerifyToken_EmptyExpectReviewer_ExpiredStillFails(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	past := time.Now().Add(-time.Hour).Unix()
	wire, _ := mintForTest(t, "alice", past)
	_, err := VerifyToken(kr, wire, "", time.Now())
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}

// TestVerifyToken_ExplicitReviewerPreservesStrictCompare guards the migration: existing callers passing an explicit reviewer keep mismatch → ErrUnverifiable.
func TestVerifyToken_ExplicitReviewerPreservesStrictCompare(t *testing.T) {
	t.Parallel()
	kr := newTestKeyring()
	wire, _ := mintForTest(t, "alice", time.Now().Add(time.Hour).Unix())
	_, err := VerifyToken(kr, wire, "bob", time.Now())
	if !errors.Is(err, ErrUnverifiable) {
		t.Fatalf("want ErrUnverifiable on explicit-reviewer mismatch, got %v", err)
	}
}

// TestExtractKID_TypedSentinels pins each kid-scan failure mode to its typed sentinel.
func TestExtractKID_TypedSentinels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"not_json_object", []byte("not-json"), ErrKIDFraming},
		{"kid_missing", []byte(`{"other":"x"}`), ErrKIDMissing},
		{"kid_invalid_utf8", []byte(`{"kid":"` + string([]byte{0xFF, 0xFE}) + `"}`), ErrKIDInvalidUTF8},
		{"kid_escape", []byte(`{"kid":"a\b"}`), ErrKIDEscape},
		{"kid_control_byte", []byte("{\"kid\":\"a\x01b\"}"), ErrKIDControl},
		{"kid_unterminated", []byte(`{"kid":"abc`), ErrKIDUnterminated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractKIDFromCanonical(tc.body)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v; want errors.Is(%v)", err, tc.want)
			}
		})
	}
}

// itoa avoids strconv import in test helper.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

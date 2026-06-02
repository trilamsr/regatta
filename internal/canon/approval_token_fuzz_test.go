package canon

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// FuzzToken_Verify asserts VerifyToken never panics and only returns typed sentinels (spec §7 A+ for #123).
func FuzzToken_Verify(f *testing.F) {
	// Seed corpus mirrors the manual test cases so the fuzzer starts
	// from known-interesting framings rather than purely-random bytes;
	// a well-formed wire reaches deep into the verify pipeline before
	// rejecting, exposing more code paths than uniformly random garbage.
	kr := newTestKeyring()
	now := time.Now()
	window := now.Add(time.Hour).Unix()

	pinned := bytes.Repeat([]byte{0x42}, 16)
	validWire, _, err := MintToken(kr, "k1", TokenPayload{
		KID: "k1", WI: "wi-1", AID: "aid-1", Reviewer: "alice", Window: window,
	}, &fixedReader{src: pinned})
	if err != nil {
		f.Fatalf("seed mint: %v", err)
	}
	f.Add(validWire)

	// Tampered payload (HMAC mismatch on otherwise-canonical body).
	dot := strings.IndexByte(validWire, '.')
	if dot > 0 {
		if pay, decErr := base64.RawURLEncoding.DecodeString(validWire[dot+1:]); decErr == nil && len(pay) > 1 {
			pay[len(pay)-2] ^= 0x01
			f.Add(validWire[:dot+1] + base64.RawURLEncoding.EncodeToString(pay))
		}
		if sig, decErr := base64.RawURLEncoding.DecodeString(validWire[:dot]); decErr == nil && len(sig) > 0 {
			sig[0] ^= 0x01
			f.Add(base64.RawURLEncoding.EncodeToString(sig) + validWire[dot:])
		}
	}

	// Malformed framing + kid-scan crafted seeds.
	for _, s := range []string{
		"",
		".",
		"abcdef",
		"aa.bb.cc",
		".aGVsbG8",
		"aGVsbG8.",
		"!!!.aGVsbG8",
		"aGVsbG8.!!!",
		"AAAA." + base64.RawURLEncoding.EncodeToString([]byte(`not-json`)),
		"AAAA." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)),
		"AAAA." + base64.RawURLEncoding.EncodeToString([]byte(`{"kid":"unknown"}`)),
		"AAAA." + base64.RawURLEncoding.EncodeToString([]byte(`{"kid":"a\b"}`)),
		"AAAA." + base64.RawURLEncoding.EncodeToString([]byte(`{"kid":"abc`)),
		"AAAA." + base64.RawURLEncoding.EncodeToString([]byte("{\"kid\":\"a\x01b\"}")),
		"AAAA." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xFF}, 64)),
	} {
		f.Add(s)
	}

	allowed := []error{ErrTokenInvalid, ErrUnknownKeyID, ErrUnverifiable, ErrTokenExpired}

	f.Fuzz(func(t *testing.T, wire string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("VerifyToken panicked on input %q: %v", wire, r)
			}
		}()
		_, err := VerifyToken(kr, wire, "alice", now)
		if err == nil {
			// Reproduced a valid seed; verify-success is allowed.
			return
		}
		for _, s := range allowed {
			if errors.Is(err, s) {
				return
			}
		}
		t.Fatalf("untyped rejection error on input %q: %v", wire, err)
	})
}

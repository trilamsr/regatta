package approvals_shadow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestBuildShadowPayload_CarriesTransitionActorAndJTI pins the substrate payload shape (#795 T5).
func TestBuildShadowPayload_CarriesTransitionActorAndJTI(t *testing.T) {
	raw, err := BuildShadowPayload("a-1", "approved", "alice", "jti-XYZ")
	if err != nil {
		t.Fatalf("BuildShadowPayload: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["approval_id"] != "a-1" {
		t.Fatalf("approval_id=%v want a-1", p["approval_id"])
	}
	if p["transition"] != "approved" {
		t.Fatalf("transition=%v want approved", p["transition"])
	}
	if p["actor"] != "alice" {
		t.Fatalf("actor=%v want alice", p["actor"])
	}
	if p["token_jti"] != "jti-XYZ" {
		t.Fatalf("token_jti=%v want jti-XYZ", p["token_jti"])
	}
}

// TestBuildShadowPayload_OmitsEmptyTokenJTI guards against empty-string column leakage (#795 T5).
func TestBuildShadowPayload_OmitsEmptyTokenJTI(t *testing.T) {
	raw, err := BuildShadowPayload("a-2", "requested", "system", "")
	if err != nil {
		t.Fatalf("BuildShadowPayload: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, has := p["token_jti"]; has {
		t.Fatalf("token_jti present on empty input; want omitted")
	}
}

// TestShadowNonce_DeterministicAcrossReplays pins the UNIQUE-constraint collision invariant (#795 T5).
func TestShadowNonce_DeterministicAcrossReplays(t *testing.T) {
	ts := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	a := ShadowNonce("a-1", "requested", "jti", ts)
	b := ShadowNonce("a-1", "requested", "jti", ts)
	if a != b {
		t.Fatalf("nonce not deterministic: %q vs %q", a, b)
	}
	if len(a) != 32 {
		t.Fatalf("nonce hex len=%d want 32 (16 bytes)", len(a))
	}
}

// TestShadowNonce_DiffersOnDifferentInputs guards against accidental collision across distinct events (#795 T5).
func TestShadowNonce_DiffersOnDifferentInputs(t *testing.T) {
	ts := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	base := ShadowNonce("a-1", "requested", "jti", ts)
	if ShadowNonce("a-2", "requested", "jti", ts) == base {
		t.Fatal("nonce collides on approvalID change")
	}
	if ShadowNonce("a-1", "approved", "jti", ts) == base {
		t.Fatal("nonce collides on transition change")
	}
	if ShadowNonce("a-1", "requested", "other", ts) == base {
		t.Fatal("nonce collides on tokenJTI change")
	}
	if ShadowNonce("a-1", "requested", "jti", ts.Add(time.Nanosecond)) == base {
		t.Fatal("nonce collides on ts change")
	}
}

// TestSanitizeWrittenBy_PassesAllowedChars asserts the substrate CHECK character class survives (#795 T5).
func TestSanitizeWrittenBy_PassesAllowedChars(t *testing.T) {
	in := "alice.bob_42:admin-1"
	if got := SanitizeWrittenBy(in); got != in {
		t.Fatalf("Sanitize(%q)=%q want unchanged", in, got)
	}
}

// TestSanitizeWrittenBy_ReplacesDisallowedChars asserts CHECK-violating chars are scrubbed (#795 T5).
func TestSanitizeWrittenBy_ReplacesDisallowedChars(t *testing.T) {
	got := SanitizeWrittenBy("alice@example.com")
	if strings.ContainsAny(got, "@") {
		t.Fatalf("Sanitize did not scrub '@': %q", got)
	}
	if got != "alice_example.com" {
		t.Fatalf("Sanitize=%q want alice_example.com", got)
	}
}

// TestSanitizeWrittenBy_EmptyFallsBackToSystem guards the not-empty CHECK (#795 T5).
func TestSanitizeWrittenBy_EmptyFallsBackToSystem(t *testing.T) {
	if got := SanitizeWrittenBy(""); got != SystemActor {
		t.Fatalf("Sanitize(\"\")=%q want %q", got, SystemActor)
	}
	if got := SanitizeWrittenBy("@@@"); got == "" {
		t.Fatalf("Sanitize fully-illegal returned empty; want non-empty fallback")
	}
}

// TestSanitizeWrittenBy_TruncatesPast128 guards the substrate column width (#795 T5).
func TestSanitizeWrittenBy_TruncatesPast128(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := SanitizeWrittenBy(long)
	if len(got) != 128 {
		t.Fatalf("Sanitize long len=%d want 128", len(got))
	}
}

// TestParseSubstrateApprovalPayload_ExtractsFields pins the read-side classifier shape (#795 T5).
func TestParseSubstrateApprovalPayload_ExtractsFields(t *testing.T) {
	raw, err := BuildShadowPayload("a-9", "approved", "bob", "jti-9")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := ParseSubstrateApprovalPayload(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.ApprovalID != "a-9" || got.Transition != "approved" || got.Actor != "bob" || got.TokenJTI != "jti-9" {
		t.Fatalf("Parse=%+v want {a-9 approved bob jti-9}", got)
	}
}

// TestParseSubstrateApprovalPayload_RejectsMalformedJSON pins error propagation (#795 T5).
func TestParseSubstrateApprovalPayload_RejectsMalformedJSON(t *testing.T) {
	if _, err := ParseSubstrateApprovalPayload([]byte("not-json")); err == nil {
		t.Fatal("expected error on malformed payload")
	}
}

// TestTruncateDiff_CapsAt512 pins the audit-row length cap (#795 T5).
func TestTruncateDiff_CapsAt512(t *testing.T) {
	in := strings.Repeat("x", 1024)
	if got := TruncateDiff(in); len(got) != 512 {
		t.Fatalf("TruncateDiff len=%d want 512", len(got))
	}
	in2 := "short"
	if got := TruncateDiff(in2); got != in2 {
		t.Fatalf("TruncateDiff(%q)=%q want passthrough", in2, got)
	}
}

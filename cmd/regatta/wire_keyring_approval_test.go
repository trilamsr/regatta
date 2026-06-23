package main

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
)

// TestApprovalGate_EmptyKeyringDeniesAll asserts approvalKeyring returns an empty MapKeyring whose Lookup fails, so the gate denies-all (R-MEGA-3 LIVE-8).
func TestApprovalGate_EmptyKeyringDeniesAll(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "")

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	emptyKeyringWarnOnce = sync.Once{}
	emptyApprovalKeyringWarnOnce = sync.Once{}

	kr, active := approvalKeyring()
	if active != "k1" {
		t.Errorf("active=%q want fallback %q", active, "k1")
	}
	if _, err := kr.Lookup("k1"); err == nil {
		t.Fatal("Lookup on empty keyring returned nil err; want fail-closed")
	}
	out := buf.String()
	if !strings.Contains(out, `surface=approval_token`) {
		t.Errorf("expected approval-surface WARN; got %q", out)
	}

	// Sanity: a non-empty MapKeyring round-trips a known kid (negative-control).
	mk := approvaltoken.MapKeyring{"k1": []byte("0123456789abcdef0123456789abcdef")}
	if _, err := mk.Lookup("k1"); err != nil {
		t.Errorf("populated keyring Lookup failed: %v", err)
	}
}

package main

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// TestLoadBriefKeyring_EmptyLogsWarn asserts loadBriefKeyringWithActive emits one WARN when both keyring env vars are unset (R-MEGA-3 LIVE-7).
func TestLoadBriefKeyring_EmptyLogsWarn(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "")

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	// Reset the once-guard so the WARN actually fires in this sub-process.
	emptyKeyringWarnOnce = sync.Once{}

	keys, active := loadBriefKeyringWithActive()
	if len(keys) != 0 || active != "" {
		t.Fatalf("want empty keyring; got keys=%d active=%q", len(keys), active)
	}
	out := buf.String()
	if !strings.Contains(out, "keyring.empty") {
		t.Errorf("expected keyring.empty WARN; got %q", out)
	}
}

// TestLoadBriefKeyring_NonEmptyDoesNotWarn asserts the WARN does not fire when the keyring is configured.
func TestLoadBriefKeyring_NonEmptyDoesNotWarn(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "")
	t.Setenv("REGATTA_HMAC_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	emptyKeyringWarnOnce = sync.Once{}

	keys, active := loadBriefKeyringWithActive()
	if len(keys) == 0 {
		t.Fatal("want non-empty keyring")
	}
	if active == "" {
		t.Error("active key id empty")
	}
	if strings.Contains(buf.String(), "keyring.empty") {
		t.Errorf("did not expect keyring.empty WARN; got %q", buf.String())
	}
}

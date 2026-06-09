package main

import (
	"strings"
	"testing"
)

// TestPreflightUIBoot_HeadlessIgnoresMissingHMAC: --ui=false (headless dispatch) returns nil regardless of REGATTA_HMAC_KEY state; covers the docker-compose REGATTA_UI=false path (#1097 c1).
func TestPreflightUIBoot_HeadlessIgnoresMissingHMAC(t *testing.T) {
	if err := preflightUIBoot(false); err != nil {
		t.Fatalf("preflightUIBoot(false) must boot regardless of HMAC, got err=%v", err)
	}
}

// TestPreflightUIBoot_TreatsEmptyAndUnsetAlike: REGATTA_HMAC_KEY="" (compose-interpolation result when host env is unset) reaches the os.Getenv != "" check the same way an unset var does; this pins the contract so a future LookupEnv refactor cannot silently regress (#1097 c5).
func TestPreflightUIBoot_TreatsEmptyAndUnsetAlike(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "")

	err := preflightUIBoot(true)
	if err == nil {
		t.Fatalf("preflightUIBoot(true) with REGATTA_HMAC_KEY=\"\" must refuse to boot")
	}
}

// TestPreflightUIBoot_UIRequiresHMAC: --ui=true with no HMAC fails fast at boot so the operator hits the misconfig before any render-time lie (#1097 c2).
func TestPreflightUIBoot_UIRequiresHMAC(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "")

	err := preflightUIBoot(true)
	if err == nil {
		t.Fatalf("preflightUIBoot(true) with missing HMAC must refuse to boot")
	}
	if !strings.Contains(err.Error(), "REGATTA_HMAC_KEY") {
		t.Fatalf("preflight err should name REGATTA_HMAC_KEY, got: %v", err)
	}
}

// TestPreflightUIBoot_UIBootsWithHMAC: --ui=true with HMAC set boots cleanly (#1097 c3).
func TestPreflightUIBoot_UIBootsWithHMAC(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "abc123")

	if err := preflightUIBoot(true); err != nil {
		t.Fatalf("preflightUIBoot(true) with HMAC set must boot, got err=%v", err)
	}
}

// TestPreflightUIBoot_UIBootsWithIndirectHMAC: REGATTA_HMAC_KEY_ENV indirection still works under --ui=true (#1097 c4).
func TestPreflightUIBoot_UIBootsWithIndirectHMAC(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "MY_KEY_HOLDER")
	t.Setenv("MY_KEY_HOLDER", "indirect-secret")

	if err := preflightUIBoot(true); err != nil {
		t.Fatalf("preflightUIBoot(true) with indirect HMAC must boot, got err=%v", err)
	}
}

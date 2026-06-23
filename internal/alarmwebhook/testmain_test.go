package alarmwebhook

import (
	"os"
	"testing"
)

// TestMain wires the WEBHOOK_AUTH_TOKEN before any test runs so the new
// LIVE-12 auth gate accepts the shared `test-token-default` post() bakes
// into every fixture request. Doing this via os.Setenv (not t.Setenv from
// inside post()) keeps the env-write off the test-parallelism path so
// TestHandler_MutexGC_RaceWithConcurrentReceive does not race (R-MEGA-3 LIVE-12).
func TestMain(m *testing.M) {
	_ = os.Setenv(WebhookAuthEnv, "test-token-default")
	os.Exit(m.Run())
}

package alarmwebhook

import (
	"os"
	"testing"
)

// TestMain wires WEBHOOK_AUTH_TOKEN via os.Setenv pre-parallelism so the new auth gate accepts fixture requests without racing the receive test (R-MEGA-3 LIVE-12).
func TestMain(m *testing.M) {
	_ = os.Setenv(WebhookAuthEnv, "test-token-default")
	os.Exit(m.Run())
}

package spawner

import (
	"bytes"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

// TestWarnIfPidfdFallbackPossible_LinuxEmits asserts the boot WARN fires on Linux (R-MEGA-2 C8).
func TestWarnIfPidfdFallbackPossible_LinuxEmits(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	warnIfPidfdFallbackPossible(log)
	out := buf.String()

	if runtime.GOOS == "linux" {
		if !strings.Contains(out, "spawner.pidfd_fallback_possible") {
			t.Fatalf("linux: missing pidfd WARN; got %q", out)
		}
	} else if out != "" {
		t.Fatalf("non-linux: unexpected WARN %q", out)
	}
}

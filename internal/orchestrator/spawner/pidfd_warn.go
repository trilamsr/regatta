package spawner

import (
	"log/slog"
	"runtime"
)

// warnIfPidfdFallbackPossible emits one boot-time WARN on Linux noting
// the PID-reuse race that lives on kernels < 5.3 where the Go runtime
// falls back from pidfd_open to syscall.Kill (R-MEGA-2 C8). The
// orchestrator targets modern kernels; the WARN is the operator-visible
// trigger to upgrade rather than a runtime mitigation.
func warnIfPidfdFallbackPossible(log *slog.Logger) {
	if runtime.GOOS != "linux" {
		return
	}
	log.Warn("spawner.pidfd_fallback_possible",
		"reason", "Linux kernels < 5.3 lack pidfd_open; os.Process.Kill targets recycled PIDs on those kernels",
		"mitigation", "deploy on kernel >= 5.3",
	)
}

package orchestrator

import (
	"os"
	"testing"
)

func TestPidAlive_RejectsZeroAndNegative(t *testing.T) {
	if pidAlive(0) {
		t.Fatal("pidAlive(0) must be false")
	}
	if pidAlive(-1) {
		t.Fatal("pidAlive(-1) must be false")
	}
	if pidAlive(-1234) {
		t.Fatal("pidAlive(-1234) must be false (stub spawner produces these)")
	}
}

func TestPidAlive_OwnProcessIsAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatalf("pidAlive(os.Getpid()=%d) must be true", os.Getpid())
	}
}

func TestPidAlive_DeadProcessIsDead(t *testing.T) {
	// PID 1 (init / launchd) is always alive on a healthy system, so
	// we cannot easily test a synthetically-dead PID without forking.
	// Pick a high PID unlikely to be in use. Tests run as a regular
	// user; if the PID happens to exist owned by another user, kill(0)
	// returns EPERM and pidAlive reports alive. Use a very large pid
	// that the kernel almost certainly hasn't allocated.
	highPID := 999_999_999
	if pidAlive(highPID) {
		t.Skipf("pid %d unexpectedly alive; skipping (test relies on a missing pid)", highPID)
	}
}

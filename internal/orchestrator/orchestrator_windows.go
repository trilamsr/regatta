//go:build !unix

package orchestrator

// osPidAlive on non-unix targets returns true conservatively: without
// a portable signal-0 probe we cannot distinguish alive from dead, so
// we never auto-requeue an agent we lack the visibility to confirm
// crashed. Production targets are Linux + macOS per docs/design.md;
// this stub exists only to keep the package buildable for tooling
// (gopls on Windows, cross-build smoke checks).
func osPidAlive(int) bool { return true }

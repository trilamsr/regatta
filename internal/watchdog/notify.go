// Package watchdog emits sd_notify WATCHDOG=1 keep-alives so systemd's
// `WatchdogSec=30` contract can restart a wedged regatta — spec §3.6.
//
// Cross-platform safe: macOS + non-Linux builds use the stub in
// notify_other.go (no-op Run). The wire format is one socket write per
// tick; no third-party dep adopted (see spec §4 — go-systemd rejected
// for size).
package watchdog

import "time"

// DefaultInterval is 10s — the spec §3.6 3x safety factor vs WatchdogSec=30.
const DefaultInterval = 10 * time.Second

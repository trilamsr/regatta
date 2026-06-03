//go:build linux

package watchdog

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"
)

// envNotifySocket is the systemd-supplied unix-datagram path; spec §3.6.
const envNotifySocket = "NOTIFY_SOCKET"

// NotifyFailuresTotal is the public counter the future /metrics scraper
// wires; package-local atomic keeps the test surface minimal.
var NotifyFailuresTotal atomic.Int64

// Notifier writes sd_notify messages on a unix-datagram socket.
// Nil notifier ⇒ NOTIFY_SOCKET was unset ⇒ Run is a clean no-op.
type Notifier struct {
	conn *net.UnixConn
	log  *slog.Logger
}

// New opens the unix-datagram socket from $NOTIFY_SOCKET. When the env
// var is unset (running under launchd, go run, tests) New returns
// (nil, nil) and Run is a no-op — spec §3.6: absence is the correct
// non-systemd signal.
func New(log *slog.Logger) (*Notifier, error) {
	if log == nil {
		log = slog.Default()
	}
	path := os.Getenv(envNotifySocket)
	if path == "" {
		return nil, nil
	}
	addr, err := net.ResolveUnixAddr("unixgram", path)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return nil, err
	}
	return &Notifier{conn: conn, log: log}, nil
}

// Run emits WATCHDOG=1 every interval until ctx.Done; emits STOPPING=1
// on shutdown for graceful-stop suppression of WatchdogSec restarts.
// Socket errors WARN-log + counter-increment + continue — never crash.
func (n *Notifier) Run(ctx context.Context, interval time.Duration) {
	if n == nil {
		<-ctx.Done()
		return
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	defer n.close()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		n.send("WATCHDOG=1")
		select {
		case <-ctx.Done():
			n.send("STOPPING=1")
			return
		case <-t.C:
		}
	}
}

func (n *Notifier) send(msg string) {
	if n == nil || n.conn == nil {
		return
	}
	_, err := n.conn.Write([]byte(msg))
	if err == nil {
		return
	}
	NotifyFailuresTotal.Add(1)
	var oe *net.OpError
	errno := ""
	if errors.As(err, &oe) {
		errno = oe.Err.Error()
	}
	n.log.Warn("sd_notify write failed", "msg", msg, "err", err, "errno", errno)
}

func (n *Notifier) close() {
	if n == nil || n.conn == nil {
		return
	}
	_ = n.conn.Close()
}

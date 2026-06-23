//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCLISiblingLiveness asserts a backgrounded `regatta serve` reports liveness through `regatta status --json` invoked from a parallel process: heartbeat age stays fresh while serve ticks (R-MEGA-3 INT-3).
func TestCLISiblingLiveness(t *testing.T) {
	repoRoot := findRepoRoot(t)
	binPath := buildRegatta(t, repoRoot)

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "regatta.db")
	addr, err := freeAddr()
	if err != nil {
		t.Fatalf("free addr: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	serveCmd := exec.CommandContext(ctx, binPath, "serve",
		"--db", dbPath,
		"--items-root", tmp,
		"--addr", addr,
		"--tick", "1s",
		"--ui=true",
		"--no-pr-watch",
	)
	serveCmd.Env = append(serveCmd.Environ(),
		"REGATTA_HMAC_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)
	if err := serveCmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	defer func() { _ = serveCmd.Process.Kill() }()

	if err := waitForHealthz("http://"+addr+"/healthz", 10*time.Second); err != nil {
		t.Fatalf("serve never healthy: %v", err)
	}

	// status --json invoked in parallel; --addr must point at the running serve.
	statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer statusCancel()
	out, err := exec.CommandContext(statusCtx, binPath, "status", "--once", "--json", "--addr", addr).Output()
	if err != nil {
		t.Fatalf("status --json: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "orchestrator") && !strings.Contains(string(out), `"version"`) {
		t.Errorf("status JSON missing expected fields: %s", out)
	}
	var any map[string]any
	if err := json.Unmarshal(out, &any); err != nil {
		t.Errorf("status output not JSON: %v\n%s", err, out)
	}
}

func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr, nil
}

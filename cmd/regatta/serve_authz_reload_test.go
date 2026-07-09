//go:build unix

// Build-tagged unix-only: drives syscall.Kill(SIGHUP). The wiring under
// test (disk.Loader + reload.Reloader) is cross-platform; the SIGHUP
// trigger surface is not, so the integration test stays unix-side.

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/testutil"
)

// TestServe_AuthzPolicyHotReload_OnSIGHUP asserts SIGHUP swaps an active policy bundle.
func TestServe_AuthzPolicyHotReload_OnSIGHUP(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	dir := t.TempDir()

	bin := filepath.Join(dir, "regatta")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "../../cmd/regatta")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build regatta: %v\n%s", err, out)
	}

	repoRoot := filepath.Join(dir, "repo")
	policyDir := filepath.Join(repoRoot, "policies")
	policyTenant := filepath.Join(policyDir, "regatta", "v1", "default")
	if err := os.MkdirAll(policyTenant, 0o755); err != nil {
		t.Fatalf("mkdir policy tenant: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".regatta", "items"), 0o755); err != nil {
		t.Fatalf("mkdir items: %v", err)
	}

	yaml := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
work_item_source:
  type: markdown_catalog
  root: .
ci:
  command: make check
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
safety:
  authz:
    policy_dir: policies
`
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}

	addr, err := freeAddr()
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	dbPath := filepath.Join(dir, "state.db")

	cmd := exec.Command(bin, "serve",
		"--spawner=stub",
		"--repo", repoRoot,
		"--items-root", repoRoot,
		"--db", dbPath,
		"--ui=true",
		"--addr", addr,
		"--poll", "1h",
		"--tick", "1h",
		"--heartbeat", "1h",
	)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "REGATTA_HMAC_KEY=integration-test-key")

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	var stderrBuf safeBuffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderrPipe)
	}()

	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-waitDone
		}
		<-stderrDone
	})

	// Wait for the listener to bind + the reloader to come up. The
	// reloader's signal handler must be registered before the test fires
	// SIGHUP; commit 6341256 gated OnStart on both watcher- and SIGHUP-
	// readiness, and buildAuthorizer logs "authz reload: ready" inside
	// that OnStart so this wait is the canonical pid-can-receive-SIGHUP
	// signal.
	waitForLogLine(t, &stderrBuf, "authz reload: ready", 5*time.Second)
	// Belt-and-braces: also confirm the listener is accepting connections.
	waitForHealthz(t, "http://"+addr+"/healthz", 5*time.Second)

	// Write a deterministically-distinct .rego under the policy dir so the
	// bundle SHA changes from the embed.FS fallback. SIGHUP then drives
	// the swap; we assert via the reloader's success log line.
	policy := `package regatta.v1.approval.view

default decision := {"allow": true, "reason": "policy-from-disk-via-sighup"}
`
	if err := os.WriteFile(filepath.Join(policyTenant, "approval.rego"), []byte(policy), 0o644); err != nil {
		t.Fatalf("write rego: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}

	waitForLogLine(t, &stderrBuf, "reload: bundle swapped", 5*time.Second)
	if !strings.Contains(stderrBuf.String(), "trigger=sighup") {
		t.Errorf("expected trigger=sighup in log; stderr=%s", stderrBuf.String())
	}
}

// freeAddr binds 127.0.0.1:0, captures the resolved address, and frees
// the listener. The kernel-assigned port is reusable for at least the
// short window the test needs.
func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String(), nil
}

// waitForHealthz polls /healthz until 200 OK or deadline. The URL is
// test-controlled (127.0.0.1 with a fresh port) — gosec G107 silence is
// the test-local trust boundary.
func waitForHealthz(t testing.TB, url string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	testutil.Eventually(t, ctx, 50*time.Millisecond, func() bool {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, "healthz timeout for "+url)
}

// waitForLogLine scans the (concurrently growing) buffer for needle.
func waitForLogLine(t testing.TB, buf *safeBuffer, needle string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	testutil.Eventually(t, ctx, 50*time.Millisecond, func() bool {
		return strings.Contains(buf.String(), needle)
	}, "log needle "+needle+" not seen within "+timeout.String())
}

// safeBuffer is a mutex-guarded bytes.Buffer for concurrent reader+writer.
// The stderr-copy goroutine writes; the polling goroutine reads.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}


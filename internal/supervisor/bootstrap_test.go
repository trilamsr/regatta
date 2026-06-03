package supervisor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner is a hermetic os/exec replacement keyed by joined args so
// each test predetermines per-command output + exit.
type fakeRunner struct {
	mu   sync.Mutex
	out  map[string][]byte
	err  map[string]error
	args [][]string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := name + " " + strings.Join(args, " ")
	f.args = append(f.args, append([]string{name}, args...))
	if e, ok := f.err[key]; ok {
		return f.out[key], e
	}
	return f.out[key], nil
}

func (f *fakeRunner) calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.args))
	copy(out, f.args)
	return out
}

// healthzServer spins up an httptest server returning the configured
// status code + body — keeps each healthz test self-contained.
func healthzServer(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stallServer accepts the connection but never responds until the test
// cleanup signals close — exercises the poll-timeout rollback path.
func stallServer(t *testing.T) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})
	return srv
}

// TestInstallService_Darwin_BootstrapInvokesLaunchctl asserts step-6 darwin path.
func TestInstallService_Darwin_BootstrapInvokesLaunchctl(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	r := &fakeRunner{}
	opts.Runner = r.run
	opts.HealthzURL = healthzServer(t, 200, `{"status":"ok"}`).URL
	opts.UID = 501
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	var found bool
	for _, c := range r.calls() {
		if len(c) >= 3 && c[0] == "launchctl" && c[1] == "bootstrap" && strings.HasPrefix(c[2], "gui/501") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected `launchctl bootstrap gui/501 ...`; got %v", r.calls())
	}
}

// TestInstallService_Linux_BootstrapInvokesSystemctl asserts step-6 linux user-mode triple.
func TestInstallService_Linux_BootstrapInvokesSystemctl(t *testing.T) {
	opts, _ := newLinuxOpts(t)
	r := &fakeRunner{}
	opts.Runner = r.run
	opts.HealthzURL = healthzServer(t, 200, `{"status":"ok"}`).URL
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	wantSubcmds := []string{"daemon-reload", "enable"}
	seen := map[string]bool{}
	for _, c := range r.calls() {
		if len(c) >= 3 && c[0] == "systemctl" && c[1] == "--user" {
			seen[c[2]] = true
		}
	}
	for _, sc := range wantSubcmds {
		if !seen[sc] {
			t.Fatalf("expected `systemctl --user %s`; got %v", sc, r.calls())
		}
	}
}

// TestInstallService_Healthz_AcceptsOK accepts 200 ok as install success.
func TestInstallService_Healthz_AcceptsOK(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.Runner = (&fakeRunner{}).run
	opts.HealthzURL = healthzServer(t, 200, `{"status":"ok"}`).URL
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(buf.String(), "healthz: ok") {
		t.Fatalf("expected healthz ok msg; got %q", buf.String())
	}
}

// TestInstallService_Healthz_AcceptsDegraded accepts 200 degraded with warning.
func TestInstallService_Healthz_AcceptsDegraded(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.Runner = (&fakeRunner{}).run
	// degraded server stays degraded for entire poll window; install
	// should still report success with a warning per spec §10 risk 11.
	opts.HealthzURL = healthzServer(t, 200, `{"status":"degraded"}`).URL
	opts.HealthzTimeout = 200 * time.Millisecond
	opts.HealthzPollInterval = 50 * time.Millisecond
	errBuf := &bytes.Buffer{}
	opts.Err = errBuf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(errBuf.String(), "degraded") {
		t.Fatalf("expected degraded warning; got stderr=%q", errBuf.String())
	}
}

// TestInstallService_Healthz_Timeout_Rollback rolls back when no response within window.
func TestInstallService_Healthz_Timeout_Rollback(t *testing.T) {
	opts, home := newDarwinOpts(t)
	opts.Runner = (&fakeRunner{}).run
	opts.HealthzURL = stallServer(t).URL
	opts.HealthzTimeout = 200 * time.Millisecond
	opts.HealthzPollInterval = 50 * time.Millisecond
	err := Install(opts)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "healthz") {
		t.Fatalf("expected healthz-rollback error; got %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	if _, statErr := os.Stat(plist); !os.IsNotExist(statErr) {
		t.Fatalf("expected unit file removed on rollback; stat err=%v", statErr)
	}
}

// TestInstallService_Healthz_Returns503_Rollback rolls back when /healthz reports down.
func TestInstallService_Healthz_Returns503_Rollback(t *testing.T) {
	opts, home := newDarwinOpts(t)
	opts.Runner = (&fakeRunner{}).run
	opts.HealthzURL = healthzServer(t, 503, `{"status":"down"}`).URL
	opts.HealthzTimeout = 200 * time.Millisecond
	opts.HealthzPollInterval = 50 * time.Millisecond
	err := Install(opts)
	if err == nil {
		t.Fatal("expected 503 rollback")
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	if _, statErr := os.Stat(plist); !os.IsNotExist(statErr) {
		t.Fatal("expected unit file removed on 503 rollback")
	}
}

// TestInstallService_BootstrapFails_RollsBackUnitFile removes the unit when bootstrap exits non-zero.
func TestInstallService_BootstrapFails_RollsBackUnitFile(t *testing.T) {
	opts, home := newLinuxOpts(t)
	r := &fakeRunner{err: map[string]error{
		"systemctl --user daemon-reload": fmt.Errorf("exit 1"),
	}}
	opts.Runner = r.run
	opts.HealthzURL = healthzServer(t, 200, `{"status":"ok"}`).URL
	err := Install(opts)
	if err == nil {
		t.Fatal("expected bootstrap error")
	}
	if !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("error should mention bootstrap; got %v", err)
	}
	unit := filepath.Join(home, ".config", "systemd", "user", "regatta.service")
	if _, statErr := os.Stat(unit); !os.IsNotExist(statErr) {
		t.Fatalf("expected unit removed; stat err=%v", statErr)
	}
}

// TestInstallService_Healthz_RetriesUntilOK polls past initial 503s.
func TestInstallService_Healthz_RetriesUntilOK(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"status":"down"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	opts, _ := newDarwinOpts(t)
	opts.Runner = (&fakeRunner{}).run
	opts.HealthzURL = srv.URL
	opts.HealthzTimeout = 5 * time.Second
	opts.HealthzPollInterval = 50 * time.Millisecond
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if hits.Load() < 3 {
		t.Fatalf("expected ≥3 poll hits; got %d", hits.Load())
	}
}

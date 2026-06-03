package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner is the os/exec seam — fakes in tests, real exec in prod —
// matched on argv key per existing prwatch/ghcli pattern.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// realRunner shells out via os/exec; the production default when
// Options.Runner is nil. The argv is built from package-internal
// constants (launchctl/systemctl + plan paths the binary just wrote),
// never operator stdin — gosec G204 is acknowledged.
func realRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // argv is internally generated, not operator-supplied
}

// defaultHealthzURL is the loopback /healthz endpoint regatta-serve
// binds when --addr defaults to :8080 (cmd/regatta/serve.go).
const defaultHealthzURL = "http://127.0.0.1:8080/healthz"

// defaultHealthzTimeout matches spec §3.1 step 7 — 30s window from
// bootstrap completion to first OK response.
const defaultHealthzTimeout = 30 * time.Second

// defaultHealthzPoll matches spec §3.1 step 7 — 1s between probes.
const defaultHealthzPoll = 1 * time.Second

// healthz status strings echoed by internal/health — duplicated as
// constants here so goconst stays quiet across the poll branches.
const (
	statusOK       = "ok"
	statusDegraded = "degraded"
)

// launchctl domain literal — also used by Mode.String when ModeSystem.
const domainSystem = "system"

// bootstrapOS dispatches to the OS-specific init-system bootstrap; on
// non-zero exit the caller rolls back the unit file so a failed
// register never leaves an orphan on disk.
func bootstrapOS(ctx context.Context, plan Plan, opts Options) error {
	switch plan.OS {
	case osDarwin:
		return bootstrapDarwin(ctx, plan, opts)
	case osLinux:
		return bootstrapLinux(ctx, plan, opts)
	}
	return fmt.Errorf("bootstrap: unsupported OS %q", plan.OS)
}

// bootstrapDarwin runs `launchctl bootstrap <domain> <plist>` — domain
// is `gui/$UID` for ModeUser and `system` for ModeSystem per spec §3.1.
func bootstrapDarwin(ctx context.Context, plan Plan, opts Options) error {
	domain := domainSystem
	if plan.Mode == ModeUser {
		uid := opts.UID
		if uid <= 0 {
			uid = os.Getuid()
		}
		domain = "gui/" + strconv.Itoa(uid)
	}
	out, err := opts.Runner(ctx, "launchctl", "bootstrap", domain, plan.UnitPath)
	if err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w: %s", domain, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// bootstrapLinux runs the systemd triple — daemon-reload + enable + the
// implicit start via `--now` — in user-mode or system-mode per plan.
func bootstrapLinux(ctx context.Context, plan Plan, opts Options) error {
	args := systemctlArgs(plan.Mode)
	if out, err := opts.Runner(ctx, "systemctl", append(args, "daemon-reload")...); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := opts.Runner(ctx, "systemctl", append(args, "enable", "--now", plan.UnitName)...); err != nil {
		return fmt.Errorf("systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// systemctlArgs returns the mode-scoped prefix; user mode adds --user
// so daemon-reload + enable target the per-user systemd manager.
func systemctlArgs(m Mode) []string {
	if m == ModeUser {
		return []string{"--user"}
	}
	return []string{}
}

// pollHealthz probes /healthz every interval until timeout — accepts
// 200 with status ∈ {ok, degraded} per spec §3.1 step 7 + risk 11.
// Returns (degraded, error): degraded=true means installed-but-not-yet-healthy,
// caller emits a warning. Error means rollback.
func pollHealthz(ctx context.Context, url string, timeout, interval time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: interval}
	var lastStatus string
	for {
		status, ok := probeHealthz(ctx, client, url)
		switch status {
		case statusOK:
			return false, nil
		case statusDegraded:
			lastStatus = statusDegraded
		default:
			lastStatus = status
		}
		if time.Now().After(deadline) || (ctx.Err() != nil) {
			if lastStatus == statusDegraded {
				return true, nil
			}
			return false, fmt.Errorf("healthz never reached ok within %s (last=%q)", timeout, lastStatus)
		}
		// non-blocking wait so ctx cancel + timeout collapse early
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
		_ = ok
	}
}

// probeHealthz returns the status string from the JSON envelope, empty
// when the server is unreachable or returns a non-2xx + non-503 code.
func probeHealthz(ctx context.Context, client *http.Client, url string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var env struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(body, &env)
	switch resp.StatusCode {
	case http.StatusOK:
		if env.Status == "" {
			return statusOK, true // assume ok when body parse fails but HTTP 200
		}
		return env.Status, true
	case http.StatusServiceUnavailable:
		if env.Status == "" {
			return "down", true
		}
		return env.Status, true
	}
	return "", false
}

// rollback reverses the partial install on bootstrap/healthz failure
// — best-effort disable + remove, so the operator's next re-run is a
// clean fresh-install path. Errors are warned, not returned: rollback
// is already on the error path and must not mask the original failure.
func rollback(ctx context.Context, plan Plan, opts Options) {
	// Best-effort disable before file removal so systemctl/launchctl
	// doesn't keep a stale registration pointing at a deleted file.
	switch plan.OS {
	case osLinux:
		args := append(systemctlArgs(plan.Mode), "disable", "--now", plan.UnitName)
		if out, err := opts.Runner(ctx, "systemctl", args...); err != nil {
			fpf(opts.Err, "WARN: rollback systemctl disable: %v: %s\n", err, strings.TrimSpace(string(out)))
		}
	case osDarwin:
		domain := domainSystem
		if plan.Mode == ModeUser {
			uid := opts.UID
			if uid <= 0 {
				uid = os.Getuid()
			}
			domain = "gui/" + strconv.Itoa(uid)
		}
		if out, err := opts.Runner(ctx, "launchctl", "bootout", domain, plan.UnitPath); err != nil {
			fpf(opts.Err, "WARN: rollback launchctl bootout: %v: %s\n", err, strings.TrimSpace(string(out)))
		}
	}
	if err := os.Remove(plan.UnitPath); err != nil && !os.IsNotExist(err) {
		fpf(opts.Err, "WARN: rollback could not remove %s: %v\n", plan.UnitPath, err)
	}
}

// resolveHealthzURL picks the operator override, falling back to the
// :8080 loopback default since regatta-serve binds defaultListenerAddr.
func resolveHealthzURL(opts Options) string {
	if opts.HealthzURL != "" {
		return opts.HealthzURL
	}
	return defaultHealthzURL
}

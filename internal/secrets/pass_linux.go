//go:build linux

package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const defaultPassBinary = "pass"

// passFetcher shells out to `pass show` on Linux. GPL-2 isolation: we
// run the unmodified upstream binary as a subprocess (no linking, no
// source modification) — regatta itself stays outside copyleft scope.
type passFetcher struct {
	binary string // pass binary path; "pass" via $PATH by default
	entry  string // optional `pass show <entry>` override; empty ⇒ use passEntryName(key) (#934)
}

// NewPassFetcher constructs the Linux pass adapter.
func NewPassFetcher(binary string) Fetcher {
	if binary == "" {
		binary = defaultPassBinary
	}
	return passFetcher{binary: binary}
}

// newNamedPassFetcher binds a canonical key to a custom pass entry path per
// spec.Name (#934). Get(_, key) still ValidateKey-guards key but issues
// `pass show <entry>` instead of `pass show <passEntryName(key)>`.
func newNamedPassFetcher(entry string) Fetcher {
	return passFetcher{binary: defaultPassBinary, entry: entry}
}

// passLookPath / passCommand are test seams so unit tests can intercept
// the `pass` binary lookup + invocation and assert which entry was queried.
var passLookPath = exec.LookPath
var passCommand = exec.CommandContext

// Name identifies this adapter in audit + diagnostics output.
func (passFetcher) Name() string { return AdapterPass }

// Get runs `pass show regatta/<short-key>`. Canonical key
// `regatta.gh_token` maps to pass entry `regatta/gh_token` (dot →
// slash). Absent entry → ErrNotFound; pass-binary absent →
// ErrUnsupported (so the chain falls through cleanly on Alpine /
// minimal containers).
func (p passFetcher) Get(ctx context.Context, key string) (Value, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	if _, err := passLookPath(p.binary); err != nil {
		return Value{}, ErrUnsupported
	}
	entry := passEntryName(key)
	if p.entry != "" {
		entry = p.entry
	}
	cmd := passCommand(ctx, p.binary, "show", entry)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `pass` exit code is non-zero on missing entry. Stderr
		// contains "is not in the password store" — match that to
		// distinguish missing-entry from real failures (gpg-agent
		// down, store corrupted).
		errMsg := stderr.String()
		if strings.Contains(errMsg, "is not in the password store") {
			return Value{}, ErrNotFound
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return Value{}, fmt.Errorf("pass show %s: %w (%s)", entry, err, strings.TrimSpace(errMsg))
		}
		return Value{}, fmt.Errorf("pass show %s: %w", entry, err)
	}
	out := bytes.TrimRight(stdout.Bytes(), "\n")
	if len(out) == 0 {
		return Value{}, ErrNotFound
	}
	return NewValue(out), nil
}

// Set writes via `pass insert -e -f`. -e reads from stdin (no echo);
// -f overwrites if present.
func (p passFetcher) Set(ctx context.Context, key string, value []byte) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if _, err := exec.LookPath(p.binary); err != nil {
		return fmt.Errorf("pass binary not found: %w", err)
	}
	entry := passEntryName(key)
	// #nosec G204 — key regex-validated above; p.binary defaults to
	// "pass" via exec.LookPath, operator-overrideable only at adapter
	// construction.
	//nolint:gosec // G204: key regex-validated above
	cmd := exec.CommandContext(ctx, p.binary, "insert", "-e", "-f", entry)
	cmd.Stdin = bytes.NewReader(value)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pass insert %s: %w (%s)", entry, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Delete removes a pass entry. -f skips the interactive confirm.
func (p passFetcher) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if _, err := exec.LookPath(p.binary); err != nil {
		return fmt.Errorf("pass binary not found: %w", err)
	}
	entry := passEntryName(key)
	// #nosec G204 — key regex-validated above; p.binary defaults to
	// "pass" via exec.LookPath, operator-overrideable only at adapter
	// construction.
	//nolint:gosec // G204: key regex-validated above
	cmd := exec.CommandContext(ctx, p.binary, "rm", "-f", entry)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pass rm %s: %w (%s)", entry, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// passEntryName maps a canonical key to its pass tree path.
func passEntryName(key string) string {
	return strings.ReplaceAll(key, ".", "/")
}

// platformAdapters returns the Linux chain: pass only. env is
// appended by Default so headless+no-gpg-agent boxes still resolve.
func platformAdapters() []Fetcher {
	return []Fetcher{NewPassFetcher("")}
}

// newNamedKeychainFetcher is unsupported on linux; returns an
// always-ErrUnsupported fetcher so routedFetcher falls through to
// Default chain (#934).
func newNamedKeychainFetcher(_ string) Fetcher { return unsupportedFetcher{adapter: AdapterKeychain} }

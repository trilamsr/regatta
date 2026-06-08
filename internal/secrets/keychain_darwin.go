//go:build darwin

package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const defaultKeychainService = "regatta"

// keychainFetcher shells out to /usr/bin/security on darwin. We avoid
// CGo + the go-keyring dep — the platform tool is documented, present
// on every darwin install, and supports the `-A` prompt-suppression
// flag we need for unattended supervisor wakes (R1).
type keychainFetcher struct {
	service string // keychain service label; "regatta" by default
	account string // optional `-a <account>` override; empty ⇒ use canonical key (#934)
}

// NewKeychainFetcher constructs the darwin Keychain adapter. service
// is the keychain "service" attribute under which secrets live.
func NewKeychainFetcher(service string) Fetcher {
	if service == "" {
		service = defaultKeychainService
	}
	return keychainFetcher{service: service}
}

// newNamedKeychainFetcher binds a canonical key to a custom keychain account
// per spec.Name (#934). Get(_, key) still ValidateKey-guards key but issues
// `security ... -a <account>` instead of `-a <key>`.
func newNamedKeychainFetcher(account string) Fetcher {
	return keychainFetcher{service: defaultKeychainService, account: account}
}

// keychainCommand is a test seam so unit tests can intercept the
// `/usr/bin/security` invocation and assert which account was queried.
var keychainCommand = exec.Command

// Name identifies this adapter for diagnostics + audit output.
func (keychainFetcher) Name() string { return AdapterKeychain }

// Get reads a generic password from the operator's login keychain.
// Missing item produces ErrNotFound; anything else is a real error
// (corrupted keychain, locked db) that aborts the chain.
func (k keychainFetcher) Get(_ context.Context, key string) (Value, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	// `security find-generic-password -s <service> -a <account> -w`
	// prints the password to stdout. -w means "print password only".
	// key is regex-validated by ValidateKey above; no shell-traversal
	// risk. #nosec G204 — fixed binary + validated args.
	account := key
	if k.account != "" {
		account = k.account
	}
	cmd := keychainCommand("/usr/bin/security", "find-generic-password", "-s", k.service, "-a", account, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// `security` exit code 44 = "The specified item could not be
		// found in the keychain." Anything else is a real failure.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return Value{}, ErrNotFound
		}
		return Value{}, fmt.Errorf("security find-generic-password: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	out := bytes.TrimRight(stdout.Bytes(), "\n")
	if len(out) == 0 {
		return Value{}, ErrNotFound
	}
	return NewValue(out), nil
}

// Set writes a generic password. -U updates if present; -A grants
// access to any application (no per-app ACL prompt — R1 mitigation,
// matches `security(1)` documented flag).
func (k keychainFetcher) Set(ctx context.Context, key string, value []byte) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	// key validated by ValidateKey; value is operator-supplied secret
	// (passing it as a flag value is the documented `security(1)`
	// pattern). #nosec G204 — fixed binary + validated key.
	cmd := exec.CommandContext(ctx, "/usr/bin/security", //nolint:gosec
		"add-generic-password", "-U", "-A",
		"-s", k.service, "-a", key, "-w", string(value),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("security add-generic-password: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Delete removes a generic password from the keychain.
func (k keychainFetcher) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	// #nosec G204 — fixed binary + validated key (see Get above).
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", //nolint:gosec
		"-s", k.service, "-a", key,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return ErrNotFound
		}
		return fmt.Errorf("security delete-generic-password: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// platformAdapters returns the darwin chain: keychain only. env is
// appended by Default so callers always see a writable fallback.
func platformAdapters() []Fetcher {
	return []Fetcher{NewKeychainFetcher("")}
}

// newNamedPassFetcher is unsupported on darwin; returns an
// always-ErrUnsupported fetcher so routedFetcher falls through to
// Default chain (#934).
func newNamedPassFetcher(_ string) Fetcher { return unsupportedFetcher{adapter: AdapterPass} }

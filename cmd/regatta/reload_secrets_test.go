package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fakeReloadDeps assembles a reloadDeps with stubbed ReadFile + Kill
// so tests do not signal the test runner itself.
func fakeReloadDeps(t *testing.T, args []string, body string, readErr error, killErr error) (reloadDeps, *bytes.Buffer, *bytes.Buffer, *int, *syscall.Signal) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	gotPID := -1
	var gotSig syscall.Signal
	d := reloadDeps{
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
		ReadFile: func(_ string) ([]byte, error) {
			if readErr != nil {
				return nil, readErr
			}
			return []byte(body), nil
		},
		Kill: func(pid int, sig syscall.Signal) error {
			gotPID = pid
			gotSig = sig
			return killErr
		},
	}
	return d, &stdout, &stderr, &gotPID, &gotSig
}

// TestReloadSecrets_SendsSIGHUPToPidFromLockfile asserts the happy path: pidfile parsed, SIGHUP delivered, exit 0 (#619).
func TestReloadSecrets_SendsSIGHUPToPidFromLockfile(t *testing.T) {
	d, stdout, _, gotPID, gotSig := fakeReloadDeps(t, []string{"--pidfile", "x.lock"}, "4242\n", nil, nil)
	if rc := runReloadSecretsWithDeps(d); rc != 0 {
		t.Fatalf("exit=%d, want 0", rc)
	}
	if *gotPID != 4242 {
		t.Fatalf("pid = %d, want 4242", *gotPID)
	}
	if *gotSig != syscall.SIGHUP {
		t.Fatalf("sig = %v, want SIGHUP", *gotSig)
	}
	if !strings.Contains(stdout.String(), "pid=4242") {
		t.Fatalf("stdout missing pid confirmation: %q", stdout.String())
	}
}

// TestReloadSecrets_MissingPidfile_Exits1 asserts a clean error path when the operator runs reload-secrets without a live serve (#619).
func TestReloadSecrets_MissingPidfile_Exits1(t *testing.T) {
	d, _, stderr, _, _ := fakeReloadDeps(t, []string{"--pidfile", "missing.lock"}, "", os.ErrNotExist, nil)
	if rc := runReloadSecretsWithDeps(d); rc != 1 {
		t.Fatalf("exit=%d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "read pidfile") {
		t.Fatalf("stderr missing read-pidfile error: %q", stderr.String())
	}
}

// TestReloadSecrets_EmptyPidfile_Exits1 covers the lockfile-without-holder case (stale .pid; no live serve).
func TestReloadSecrets_EmptyPidfile_Exits1(t *testing.T) {
	d, _, stderr, _, _ := fakeReloadDeps(t, []string{"--pidfile", "stale.lock"}, "\n", nil, nil)
	if rc := runReloadSecretsWithDeps(d); rc != 1 {
		t.Fatalf("exit=%d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "pidfile empty") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

// TestReloadSecrets_InvalidPid_Exits1 guards against a corrupted lockfile.
func TestReloadSecrets_InvalidPid_Exits1(t *testing.T) {
	d, _, stderr, _, _ := fakeReloadDeps(t, []string{"--pidfile", "bad.lock"}, "not-a-pid\n", nil, nil)
	if rc := runReloadSecretsWithDeps(d); rc != 1 {
		t.Fatalf("exit=%d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "invalid pid") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

// TestReloadSecrets_KillFailureSurfaces propagates a kill error (e.g. ESRCH — pid no longer running) to exit 1.
func TestReloadSecrets_KillFailureSurfaces(t *testing.T) {
	d, _, stderr, _, _ := fakeReloadDeps(t, []string{"--pidfile", "p.lock"}, "1234", nil, syscall.ESRCH)
	if rc := runReloadSecretsWithDeps(d); rc != 1 {
		t.Fatalf("exit=%d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "kill SIGHUP") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

// TestReloadSecrets_RealLockfileRoundTrip exercises the real os.ReadFile path on a tempfile, with a stubbed Kill so the test process is unsignalled.
func TestReloadSecrets_RealLockfileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regatta.db.lock")
	if err := os.WriteFile(path, []byte("9999"), 0o600); err != nil {
		t.Fatalf("setup write: %v", err)
	}
	var stdout, stderr bytes.Buffer
	got := -1
	d := reloadDeps{
		Args:     []string{"--pidfile", path},
		Stdout:   &stdout,
		Stderr:   &stderr,
		ReadFile: os.ReadFile,
		Kill: func(pid int, _ syscall.Signal) error {
			got = pid
			return nil
		},
	}
	if rc := runReloadSecretsWithDeps(d); rc != 0 {
		t.Fatalf("exit=%d, want 0 (stderr=%q)", rc, stderr.String())
	}
	if got != 9999 {
		t.Fatalf("pid = %d, want 9999", got)
	}
}

// TestReloadSecrets_ParsePID_RejectsNegative pins the parsePID guard against negative-pid lockfiles.
func TestReloadSecrets_ParsePID_RejectsNegative(t *testing.T) {
	if _, err := parsePID([]byte("-1")); err == nil {
		t.Fatalf("parsePID(-1) accepted; want reject")
	}
	if _, err := parsePID([]byte("0")); err == nil {
		t.Fatalf("parsePID(0) accepted; want reject")
	}
}

// ensure errors.Is wiring is exercised — gosec linter sometimes
// removes unused imports if a helper is the only consumer.
var _ = errors.Is

// TestReloadSecrets_DefaultPidfileHonorsStateDBEnv asserts REGATTA_STATE_DB overrides flow through into the default --pidfile so docker compose pinning --db /data/regatta.db automatically points the lockfile reader at /data/regatta.db.lock.
func TestReloadSecrets_DefaultPidfileHonorsStateDBEnv(t *testing.T) {
	t.Setenv("REGATTA_STATE_DB", "/tmp/custom/state.db")
	var stderr strings.Builder
	rc := runReloadSecretsWithDeps(reloadDeps{
		Args:     nil,
		Stdout:   io.Discard,
		Stderr:   &stderr,
		ReadFile: func(p string) ([]byte, error) { return nil, fmt.Errorf("stat %s: no such file or directory", p) },
		Kill:     func(int, syscall.Signal) error { return nil },
	})
	_ = rc
	want := "/tmp/custom/state.db.lock"
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr lacks expected default pidfile %q (REGATTA_STATE_DB-derived); got %q", want, stderr.String())
	}
}

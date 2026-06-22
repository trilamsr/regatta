// reload_secrets.go is the thin operator wrapper closing #619. It
// reads the supervisor's PID from the lockfile that `regatta serve`
// already maintains beside its sqlite DB (see internal/orchestrator/
// lockfile) and sends SIGHUP — the same signal Cache.Run listens for.
// Operators previously had to type `kill -HUP $(pgrep regatta-serve)`;
// the wrapper is ~30 LoC and removes the pgrep + pidfile-path guess
// step from the rotation drill (spec §7).
//
// Decision per CLAUDE.md (UX > best-practices): in-process
// atomic-pointer rotation is the canonical path; SIGHUP triggers it
// without a re-exec. Spec §5 wording "re-exec the child process tree"
// is amended in this same PR (see spec footnote at §5) — see #654.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const subcmdReloadSecrets = "reload-secrets"

// reloadDeps bundles IO + the signal/file primitives so tests can
// drive the path deterministically (no real SIGHUP to the test
// process).
type reloadDeps struct {
	Args     []string
	Stdout   io.Writer
	Stderr   io.Writer
	ReadFile func(path string) ([]byte, error)
	Kill     func(pid int, sig syscall.Signal) error
}

// runReloadSecrets dispatches `regatta reload-secrets`.
func runReloadSecrets(args []string) int {
	return runReloadSecretsWithDeps(reloadDeps{
		Args:     args,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		ReadFile: os.ReadFile,
		Kill:     syscall.Kill,
	})
}

// runReloadSecretsWithDeps is the testable entry. Exit 0 on
// signal-sent, 1 on missing pidfile or kill failure, 2 on usage error.
func runReloadSecretsWithDeps(d reloadDeps) int {
	fs := flag.NewFlagSet("reload-secrets", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	// Default lockfile path follows defaultStateDB() so REGATTA_STATE_DB
	// overrides flow through automatically (docker compose pins --db
	// /data/regatta.db -> the lockfile lands at /data/regatta.db.lock,
	// per orchestrator lockfile convention `<dbPath> + ".lock"`).
	pidPath := fs.String("pidfile", defaultStateDB()+".lock", "path to regatta-serve lockfile (PID written under the flock)")
	if err := fs.Parse(d.Args); err != nil {
		return 2
	}
	raw, err := d.ReadFile(*pidPath)
	if err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta reload-secrets: read pidfile %s: %v\n", *pidPath, err)
		return 1
	}
	pid, err := parsePID(raw)
	if err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta reload-secrets: %s: %v\n", *pidPath, err)
		return 1
	}
	if err := d.Kill(pid, syscall.SIGHUP); err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta reload-secrets: kill SIGHUP pid=%d: %v\n", pid, err)
		return 1
	}
	_, _ = fmt.Fprintf(d.Stdout, "regatta reload-secrets: sent SIGHUP to pid=%d (pidfile=%s)\n", pid, *pidPath)
	return 0
}

// parsePID trims the lockfile body (PID written without trailing
// newline by lockfile.Acquire) and parses to int. Empty content
// surfaces a distinct error so operators see "no holder running"
// rather than a strconv stack.
func parsePID(raw []byte) (int, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, fmt.Errorf("pidfile empty (no regatta-serve running)")
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid %q", s)
	}
	return pid, nil
}

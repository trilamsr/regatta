package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/gitenv"
	_ "modernc.org/sqlite"
)

// TestServeWithClaudeSpawnerEndToEnd builds the regatta binary,
// points it at a temp repo, and asserts that
// `serve --spawner=claude --tick-once` spawns a fake-claude shim
// into a worktree, captures a real PID, and reaches the `running`
// state. The fake-claude shim is killed deterministically via the
// captured PID at teardown.
//
// Uses modernc.org/sqlite directly to read the agent row so the
// test does not require the sqlite3 CLI on PATH. Skipped on
// windows because the shim is a bash script.
func TestServeWithClaudeSpawnerEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash shim is unix-only")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()

	bin := filepath.Join(dir, "regatta")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "../../cmd/regatta")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build regatta: %v\n%s", err, out)
	}

	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".regatta", "items"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, c := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init", "-q"},
	} {
		cmd := exec.Command("git", c...)
		cmd.Dir = repo
		cmd.Env = gitenv.Scrub(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}

	item := `---
id: DEMO-1
title: end-to-end claude spawn
kind: feature
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: demo criterion
`
	if err := os.WriteFile(filepath.Join(repo, ".regatta", "items", "001.md"), []byte(item), 0o644); err != nil {
		t.Fatalf("write item: %v", err)
	}

	shim := filepath.Join(dir, "fake-claude")
	shimBody := "#!/usr/bin/env bash\ncat > /dev/null\nsleep 30\n"
	if err := os.WriteFile(shim, []byte(shimBody), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}

	dbPath := filepath.Join(dir, "state.db")
	serve := exec.Command(bin, "serve",
		"--spawner=claude",
		"--repo", repo,
		"--items-root", repo,
		"--db", dbPath,
		"--claude", shim,
		"--base-ref", "HEAD",
		"--tick-once",
	)
	var stderr bytes.Buffer
	serve.Stderr = &stderr
	if err := serve.Run(); err != nil {
		t.Fatalf("serve: %v\nstderr=%s", err, stderr.String())
	}

	wtPath := filepath.Join(repo, ".regatta", "worktrees", "agent-1")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}

	// Read the agent row via modernc.org/sqlite directly so the
	// test does not depend on the sqlite3 CLI being installed.
	pid := readRunningAgentPID(t, dbPath, "DEMO-1")

	// Deterministic teardown: kill the shim by its real pid so the
	// test never leaks a 30s sleep. SIGKILL because the shim's bash
	// loop ignores SIGTERM in some shells.
	t.Cleanup(func() {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(syscall.SIGKILL)
		}
		// Give the kernel a moment to reap; then assert the
		// process is gone. Best-effort; a stuck child is logged but
		// not failed (the test temp dir cleanup handles the rest).
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(pid, 0); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("warning: shim pid %d still alive at cleanup", pid)
	})
}

func readRunningAgentPID(t *testing.T, dbPath, workItemID string) int {
	t.Helper()
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var state string
	var pid int
	row := db.QueryRowContext(ctx, "SELECT state, pid FROM agents WHERE work_item_id = ?", workItemID)
	if err := row.Scan(&state, &pid); err != nil {
		t.Fatalf("scan agent row: %v", err)
	}
	if state != "running" {
		t.Fatalf("agent state=%q, want running", state)
	}
	if pid <= 0 {
		t.Fatalf("expected positive real pid, got %d", pid)
	}
	return pid
}

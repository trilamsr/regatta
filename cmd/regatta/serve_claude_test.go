package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestServeWithClaudeSpawnerEndToEnd is a smoke test that builds the
// regatta binary, points it at a temp repo, and asserts that
// `serve --spawner=claude --tick-once` spawns a fake-claude binary
// into a worktree, captures a real PID, and reaches the `running`
// state.
//
// Skipped on non-unix platforms because the fake-claude shim is a
// bash script.
func TestServeWithClaudeSpawnerEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash shim is unix-only")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		// sqlite3 is only used for the final state assertion below;
		// the orchestrator itself uses modernc.org/sqlite.
		t.Skip("sqlite3 CLI not on PATH")
	}

	dir := t.TempDir()

	// Build the regatta binary once into the temp dir.
	bin := filepath.Join(dir, "regatta")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/regatta")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build regatta: %v\n%s", err, out)
	}

	// Prepare a fresh git repo as the agent's worktree base.
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
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}

	item := `---
id: DEMO-1
title: end-to-end claude spawn
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

	// Worktree must exist.
	wtPath := filepath.Join(repo, ".regatta", "worktrees", "agent-1")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}

	// Agent row must be `running` with a positive pid (real shim).
	out, err := exec.Command("sqlite3", dbPath, "select state, pid from agents where work_item_id='DEMO-1'").CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "running|") {
		t.Fatalf("agent row %q; want state=running", got)
	}
	parts := strings.Split(got, "|")
	if len(parts) != 2 {
		t.Fatalf("unexpected sqlite output %q", got)
	}
	var pid int
	if _, err := fmt.Sscanf(parts[1], "%d", &pid); err != nil {
		t.Fatalf("parse pid %q: %v", parts[1], err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive real pid, got %d", pid)
	}

	// Reap the shim so the test does not leak processes.
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}

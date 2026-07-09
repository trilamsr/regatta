//go:build unix

// Build-tagged unix-only: matches serve_claude_test — the test drives
// the real `regatta serve --tick-once` binary, and the orchestrator's
// lockfile contract is POSIX-only.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	_ "modernc.org/sqlite"
)

// TestServe_YAMLWorkItemSourceRoot_DrivesItemsCatalog pins the S1-T1 contract: when `regatta.yaml` declares  work_item_source: type: markdown_catalog
func TestServe_YAMLWorkItemSourceRoot_DrivesItemsCatalog(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	dir := t.TempDir()

	bin := filepath.Join(dir, "regatta")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "../../cmd/regatta")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build regatta: %v\n%s", err, out)
	}

	// Layout: repo root holds regatta.yaml; itemsRoot is a sibling
	// subdir under the same repo so the resolution `repoRoot +
	// work_item_source.root` lands inside the temp tree.
	repoRoot := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".regatta", "items"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
safety: {}
`
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}

	item := `---
id: YAML-DRIVES-1
title: yaml resolves items root
kind: feature
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: yaml root drove this listing
`
	if err := os.WriteFile(filepath.Join(repoRoot, ".regatta", "items", "001.md"), []byte(item), 0o644); err != nil {
		t.Fatalf("write item: %v", err)
	}

	dbPath := filepath.Join(dir, "state.db")
	serve := exec.Command(bin, "serve",
		"--spawner=stub",
		"--repo", repoRoot,
		"--db", dbPath,
		"--tick-once",
		"--ui=false",
		// NB: no --items-root. The yaml's work_item_source.root must drive it.
	)
	// The CWD must be the repo root so `regatta serve` finds
	// regatta.yaml via its existing repoRoot-relative load
	// (buildApprovalGate already does this).
	serve.Dir = repoRoot
	var stderr, stdout bytes.Buffer
	serve.Stderr = &stderr
	serve.Stdout = &stdout
	if err := serve.Run(); err != nil {
		t.Fatalf("serve: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}

	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items WHERE id = ?", "YAML-DRIVES-1")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 1 {
		t.Fatalf("work_items row count for YAML-DRIVES-1 = %d; want 1 (yaml work_item_source.root failed to drive items-root)\nstderr=%s\nstdout=%s", count, stderr.String(), stdout.String())
	}
}

// TestServe_YAMLWorkItemSourceRoot_ExplicitFlagOverrides pins the resolution priority: an explicit `--items-root` beats the yaml-declared `spec_a
func TestServe_YAMLWorkItemSourceRoot_ExplicitFlagOverrides(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	dir := t.TempDir()

	bin := filepath.Join(dir, "regatta")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "../../cmd/regatta")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build regatta: %v\n%s", err, out)
	}

	// Two candidate roots; yaml points at "yaml-root" (which is empty);
	// --items-root flag points at "flag-root" (which holds the seed).
	repoRoot := filepath.Join(dir, "repo")
	yamlRoot := filepath.Join(repoRoot, "yaml-root")
	flagRoot := filepath.Join(repoRoot, "flag-root")
	if err := os.MkdirAll(filepath.Join(yamlRoot, ".regatta", "items"), 0o755); err != nil {
		t.Fatalf("mkdir yaml-root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(flagRoot, ".regatta", "items"), 0o755); err != nil {
		t.Fatalf("mkdir flag-root: %v", err)
	}

	yaml := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
work_item_source:
  type: markdown_catalog
  root: yaml-root
ci:
  command: make check
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
safety: {}
`
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}

	item := `---
id: FLAG-WINS-1
title: explicit flag overrides yaml
kind: feature
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: flag-root drove this listing
`
	if err := os.WriteFile(filepath.Join(flagRoot, ".regatta", "items", "001.md"), []byte(item), 0o644); err != nil {
		t.Fatalf("write item: %v", err)
	}

	dbPath := filepath.Join(dir, "state.db")
	serve := exec.Command(bin, "serve",
		"--spawner=stub",
		"--repo", repoRoot,
		"--items-root", flagRoot,
		"--db", dbPath,
		"--tick-once",
		"--ui=false",
	)
	serve.Dir = repoRoot
	var stderr bytes.Buffer
	serve.Stderr = &stderr
	if err := serve.Run(); err != nil {
		t.Fatalf("serve: %v\nstderr=%s", err, stderr.String())
	}

	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items WHERE id = ?", "FLAG-WINS-1")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 1 {
		t.Fatalf("work_items row count for FLAG-WINS-1 = %d; want 1 (explicit --items-root flag failed to override yaml)", count)
	}
}

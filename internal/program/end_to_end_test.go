package program_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

// TestEndToEnd_PlanAndServe exercises the MVP-1 acceptance flow:
// regatta program plan --write -> regatta serve --tick-once ->
// 3 brief-child work_items rows + 3 running feature agents.
//
// Per spec §6 series-complete DoD #5. Offline: -planner=stub +
// -spawner=stub keep the run hermetic; no Anthropic key or live
// process is touched.
func TestEndToEnd_PlanAndServe(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test; skip with -short")
	}
	repoRoot := reporoot.Must(t)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "regatta")
	build := exec.Command("go", "build", "-o", bin, "./cmd/regatta")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, ".regatta", "programs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, ".regatta", "items"), 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "program", "PROG-1.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	itemPath := filepath.Join(target, ".regatta", "items", "PROG-1.md")
	if err := os.WriteFile(itemPath, fixture, 0o600); err != nil { // #nosec G304,G703 — itemPath is a fixed join under t.TempDir(); no caller-influenced input.
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"HMAC_KEY=test-key-32-bytes-aaaaaaaaaaaaaaa",
		"REGATTA_HMAC_KEY=test-key-32-bytes-aaaaaaaaaaaaaaa",
	)

	plan := exec.Command(bin, "program", "plan",
		"-hmac-key-env=HMAC_KEY",
		"-planner=stub",
		"-write",
		itemPath)
	plan.Dir = target
	plan.Env = env
	if out, err := plan.CombinedOutput(); err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}

	serve := exec.Command(bin, "serve",
		"-spawner=stub",
		"-tick-once",
		"-repo", target,
		"-items-root", target,
		"-db", filepath.Join(target, "state.db"))
	serve.Dir = target
	serve.Env = env
	if out, err := serve.CombinedOutput(); err != nil {
		t.Fatalf("serve: %v\n%s", err, out)
	}

	assertCounts(t, filepath.Join(target, "state.db"))
}

// assertCounts opens the state DB read-only and asserts the spec §6
// shape: 3 child work_items + 3 running feature agents. Uses
// state.Open + the native sql driver instead of shelling out to
// `sqlite3` so the test stays portable across CI images that lack the
// CLI.
func assertCounts(t *testing.T, dbPath string) {
	t.Helper()
	ctx := context.Background()
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wantChildren := 3
	gotChildren := scanCount(t, db.SQL(),
		`SELECT COUNT(*) FROM work_items WHERE parent_program_id = ?`, "PROG-1")
	if gotChildren != wantChildren {
		t.Fatalf("work_items children of PROG-1: got %d, want %d", gotChildren, wantChildren)
	}

	wantRunning := 3
	gotRunning := scanCount(t, db.SQL(), `
		SELECT COUNT(*) FROM agents a
		JOIN work_items w ON w.id = a.work_item_id
		WHERE a.state = 'running' AND w.parent_program_id = ?`, "PROG-1")
	if gotRunning != wantRunning {
		t.Fatalf("running agents for PROG-1 children: got %d, want %d", gotRunning, wantRunning)
	}
}

func scanCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}


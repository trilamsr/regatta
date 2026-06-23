package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckGitdirReachable_RegularGitDirectoryPasses: normal repo with .git/ as a directory returns nil (#1095 c1).
func TestCheckGitdirReachable_RegularGitDirectoryPasses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := checkGitdirReachable(root); err != nil {
		t.Fatalf("regular .git dir should pass: %v", err)
	}
}

// TestCheckGitdirReachable_MissingDotGitPasses: zero-config fixture with no .git at all passes through so smoke-test setups still boot (#1095 c2).
func TestCheckGitdirReachable_MissingDotGitPasses(t *testing.T) {
	root := t.TempDir()
	if err := checkGitdirReachable(root); err != nil {
		t.Fatalf("missing .git should pass: %v", err)
	}
}

// TestCheckGitdirReachable_BrokenGitdirPointerFails: worktree-mount with .git as a pointer to an unreachable host path returns a named error so the operator sees the misconfig at boot instead of every spawn dying with `fatal: not a git repository` (#1095 c3).
func TestCheckGitdirReachable_BrokenGitdirPointerFails(t *testing.T) {
	root := t.TempDir()
	dotGit := filepath.Join(root, ".git")
	unreachable := "/this/path/does/not/exist/anywhere"
	if err := os.WriteFile(dotGit, []byte("gitdir: "+unreachable+"\n"), 0o600); err != nil {
		t.Fatalf("write .git pointer: %v", err)
	}
	err := checkGitdirReachable(root)
	if err == nil {
		t.Fatalf("broken gitdir pointer should fail")
	}
	if !strings.Contains(err.Error(), "gitdir") || !strings.Contains(err.Error(), unreachable) {
		t.Fatalf("error should name gitdir + path; got: %v", err)
	}
}

// TestCheckGitdirReachable_ReachableGitdirPointerPasses: legitimate worktree usage where the gitdir target IS reachable still passes — a host running regatta natively from inside a git worktree must not be blocked (#1095 c4).
func TestCheckGitdirReachable_ReachableGitdirPointerPasses(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+target+"\n"), 0o600); err != nil {
		t.Fatalf("write .git pointer: %v", err)
	}
	if err := checkGitdirReachable(root); err != nil {
		t.Fatalf("reachable gitdir pointer should pass: %v", err)
	}
}

// TestCheckGitdirReachable_NonGitdirFilePasses: someone wrote a non-gitdir line into .git (unlikely but plausible); not our problem, pass through and let git itself decide (#1095 c5).
func TestCheckGitdirReachable_NonGitdirFilePasses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("not a gitdir line\n"), 0o600); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	if err := checkGitdirReachable(root); err != nil {
		t.Fatalf("non-gitdir .git file should pass: %v", err)
	}
}

// TestCheckGitdirReachable_OversizeGitFilePasses: a .git pointer file larger than the read cap is truncated before regex match — the function does not exhaust memory on a maliciously large file (#1095 c6).
func TestCheckGitdirReachable_OversizeGitFilePasses(t *testing.T) {
	root := t.TempDir()
	// Write 2× the read cap; no `gitdir:` token in the first 64 KiB so the
	// regex misses and we pass through to "let git decide" — the point is
	// just that ReadAll didn't try to load the whole file.
	junk := make([]byte, gitdirPointerReadCap*2)
	for i := range junk {
		junk[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), junk, 0o600); err != nil {
		t.Fatalf("write oversize .git: %v", err)
	}
	if err := checkGitdirReachable(root); err != nil {
		t.Fatalf("oversize .git should not error: %v", err)
	}
}

// TestCheckRepoRootExists_MissingDirFails asserts --repo /nonexistent fails preflight loud (round-3 finding: silently dispatched against cwd before fix).
func TestCheckRepoRootExists_MissingDirFails(t *testing.T) {
	err := checkRepoRootExists("/tmp/regatta-r3-nonexistent-path-12345")
	if err == nil {
		t.Fatal("checkRepoRootExists on missing path returned nil; want loud error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %q; want 'does not exist' substring", err.Error())
	}
}

// TestCheckRepoRootExists_FilePathFails asserts a file path (not directory) gets rejected.
func TestCheckRepoRootExists_FilePathFails(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "i-am-a-file")
	if err := os.WriteFile(filePath, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkRepoRootExists(filePath)
	if err == nil {
		t.Fatal("checkRepoRootExists on file path returned nil; want error")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %q; want 'not a directory'", err.Error())
	}
}

// TestCheckRepoRootExists_RealDirPasses asserts an existing directory passes.
func TestCheckRepoRootExists_RealDirPasses(t *testing.T) {
	if err := checkRepoRootExists(t.TempDir()); err != nil {
		t.Fatalf("checkRepoRootExists(tempdir) = %v; want nil", err)
	}
}

// TestPreflight_AcceptsWorktreeRepo asserts relative gitdir pointer resolves against repoRoot (R-MEGA-2 P1).
func TestPreflight_AcceptsWorktreeRepo(t *testing.T) {
	root := t.TempDir()
	mainGit := filepath.Join(root, "..", "main-git")
	if err := os.MkdirAll(mainGit, 0o755); err != nil {
		t.Fatalf("mkdir main: %v", err)
	}
	wtDir := filepath.Join(mainGit, "worktrees", "agent-1")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree dir: %v", err)
	}
	rel, err := filepath.Rel(root, wtDir)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+rel+"\n"), 0o600); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	if err := checkGitdirReachable(root); err != nil {
		t.Fatalf("worktree pointer with reachable relative gitdir should pass: %v", err)
	}
}

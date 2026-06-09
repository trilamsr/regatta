package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const gitdirPointerReadCap = 64 << 10

// runBootPreflights executes the loud-at-boot gates that must pass before any DB open: UI-HMAC contract + git-repo-root reachability. Centralising the two checks here keeps serve.go below the file-size ceiling (#737).
func runBootPreflights(f serveFlags, logger *log.Logger) error {
	if err := preflightUIBoot(f.UI); err != nil {
		logger.Printf("%v", err)
		return err
	}
	if err := checkGitdirReachable(f.RepoRoot); err != nil {
		logger.Printf("repo preflight: %v", err)
		return err
	}
	return nil
}

var gitdirPointerRx = regexp.MustCompile(`(?m)^gitdir:\s+(.+)$`)

// checkGitdirReachable rejects mounting a git worktree (whose .git is a `gitdir:` pointer file referencing a host path absent inside the container) so every subsequent `git worktree add` does not fail silently per #1095. A regular .git directory or missing .git both pass through — the latter so smoke-test fixtures without a git ancestor still boot.
func checkGitdirReachable(repoRoot string) error {
	gitPath := filepath.Join(repoRoot, ".git")
	st, err := os.Stat(gitPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", gitPath, err)
	}
	if st.IsDir() {
		return nil
	}
	f, err := os.Open(gitPath) //nolint:gosec // gitPath is repoRoot + ".git"; repoRoot is the operator-supplied --repo flag
	if err != nil {
		return fmt.Errorf("open %s: %w", gitPath, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, gitdirPointerReadCap))
	if err != nil {
		return fmt.Errorf("read %s: %w", gitPath, err)
	}
	m := gitdirPointerRx.FindSubmatch(data)
	if m == nil {
		return nil
	}
	target := strings.TrimSpace(string(m[1]))
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("repo root %s is a git worktree whose gitdir %q is not reachable from this process; mount the main repo path, not a worktree", repoRoot, target)
		}
		return fmt.Errorf("stat gitdir target %s: %w", target, err)
	}
	return nil
}

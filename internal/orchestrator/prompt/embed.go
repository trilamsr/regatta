// Package prompt resolves the operating-rules text the spawner injects into
// each worker subprocess. Resolution order is target-repo file first,
// bundled default second — so a target with no CLAUDE.md still gets a
// repo-agnostic baseline, and a target with a CLAUDE.md is honored verbatim.
package prompt

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets/CLAUDE.md.default assets/dispatch-templates/*.default.md
var bundled embed.FS

// Source identifies which file produced the resolved prompt text so callers
// can log which path won without a second filesystem stat.
type Source string

const (
	// SourceTarget means the target repo's own CLAUDE.md was used verbatim.
	SourceTarget Source = "target"
	// SourceBundled means the bundled default shipped inside the regatta binary was used.
	SourceBundled Source = "bundled"
)

// ResolveClaudeMd reads `<repoRoot>/CLAUDE.md` if present and returns it verbatim;
// otherwise it returns the bundled default. An empty repoRoot skips the disk
// read and goes straight to bundled — that matches the spawner contract for
// isolated unit tests where Request.RepoRoot is unset.
func ResolveClaudeMd(repoRoot string) (string, Source, error) {
	if repoRoot != "" {
		path := filepath.Join(repoRoot, "CLAUDE.md")
		b, err := os.ReadFile(path) // #nosec G304 — repoRoot is the operator-configured target worktree; reading its CLAUDE.md is the documented contract.
		if err == nil {
			return string(b), SourceTarget, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
	}
	b, err := bundled.ReadFile("assets/CLAUDE.md.default")
	if err != nil {
		return "", "", err
	}
	return string(b), SourceBundled, nil
}

// AllBundledAssets returns every embedded asset keyed by its on-disk path
// (relative to `assets/prompts/`). Lint and audit code use this to scan
// every shipped default for repo-specific leakage in one pass.
func AllBundledAssets() map[string]string {
	out := map[string]string{}
	_ = fs.WalkDir(bundled, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, readErr := bundled.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[path] = string(b)
		return nil
	})
	return out
}

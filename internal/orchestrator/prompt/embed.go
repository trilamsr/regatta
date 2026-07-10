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

// Source tags which file produced the resolved prompt text so callers
// can distinguish target-worktree overrides from the bundled defaults.
type Source string

// SourceTarget is set when the prompt was read from the target worktree.
const SourceTarget Source = "target"

// SourceBundled is set when the prompt fell through to the embedded default.
const SourceBundled Source = "bundled"

// ResolveClaudeMd returns the target's CLAUDE.md when present, else the bundled default; empty repoRoot skips disk (unit-test contract).
func ResolveClaudeMd(repoRoot string) (string, Source, error) {
	if repoRoot != "" {
		path := filepath.Join(repoRoot, "CLAUDE.md")
		b, err := os.ReadFile(path) // #nosec G304 -- operator-configured target worktree
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

// AllBundledAssets returns every embedded asset keyed by on-disk path for repo-leakage audits.
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

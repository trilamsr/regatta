// Package prompt resolves operating-rules text the spawner injects into worker subprocesses; target-repo CLAUDE.md wins over the bundled default.
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

type Source string //nolint:revive // tag for which file produced the resolved text

const (
	SourceTarget  Source = "target"  //nolint:revive // resolution outcome
	SourceBundled Source = "bundled" //nolint:revive // resolution outcome
)

func ResolveClaudeMd(repoRoot string) (string, Source, error) { //nolint:revive // returns target CLAUDE.md when present, else bundled default; empty repoRoot skips disk
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

func AllBundledAssets() map[string]string { //nolint:revive // every embedded asset keyed by on-disk path for repo-leakage audits
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

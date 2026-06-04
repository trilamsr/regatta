package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
)

// buildItemBodyLoader rescans <repoRoot>/.regatta/items per call so briefs added mid-loop become visible without a daemon restart; misses fall through to ScheduleOnce's WARN-and-degrade path.
func buildItemBodyLoader(repoRoot string, slogger *slog.Logger) func(ctx context.Context, workItemID string) (string, bool) {
	dir := filepath.Join(repoRoot, ".regatta", "items")
	var mu sync.Mutex
	return func(ctx context.Context, workItemID string) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		if err := ctx.Err(); err != nil {
			return "", false
		}
		return scanItemsForID(dir, workItemID, slogger)
	}
}

// maxItemBodyBytes caps a single brief at 256KB so prompt-bloat (and downstream token cost) stays bounded; oversize files fall through to the WARN-and-degrade path.
const maxItemBodyBytes = 256 * 1024

func scanItemsForID(dir, workItemID string, slogger *slog.Logger) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && slogger != nil {
			slogger.Warn("item_body_loader.readdir_failed", "dir", dir, "err", err.Error())
		}
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "_") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		// Symlinks could redirect reads to ~/.ssh/id_rsa or any out-of-tree secret, so the loader refuses to follow them at all.
		if info.Mode()&os.ModeSymlink != 0 {
			if slogger != nil {
				slogger.Warn("item_body_loader.symlink_skipped", "path", path)
			}
			continue
		}
		if info.Size() > maxItemBodyBytes {
			if slogger != nil {
				slogger.Warn("item_body_loader.oversize_skipped", "path", path, "bytes", info.Size(), "cap", maxItemBodyBytes)
			}
			continue
		}
		// #nosec G304 — path = flag-constrained repoRoot + fixed .regatta/items + dir entry; Lstat above rejects symlinks before this read.
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parsed, err := adapter.ParseMarkdownItem(data)
		if err != nil {
			continue
		}
		if string(parsed.ID) == workItemID {
			return string(data), true
		}
	}
	return "", false
}

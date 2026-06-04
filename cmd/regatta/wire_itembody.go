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
		// #nosec G304 — path = flag-constrained repoRoot + fixed .regatta/items + dir entry; same trust boundary as adapter/markdown.go.
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

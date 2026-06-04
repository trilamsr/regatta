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

// buildItemBodyLoader resolves work_item IDs to raw markdown briefs
// under <repoRoot>/.regatta/items. The directory is rescanned per call
// so briefs added mid-loop (issue auto-triage) become visible without
// a daemon restart — bounded by item count which the self-host loop
// keeps small. Returns ("",false) on miss so ScheduleOnce's
// WARN-and-degrade path stays intact.
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

// scanItemsForID walks dir for the first *.md whose frontmatter id
// matches workItemID. ParseMarkdownItem enforces the same frontmatter
// schema the orchestrator's adapter uses, so a hit here is the same
// brief mirrored into work_items.
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
		data, err := os.ReadFile(path) // #nosec G304 — path is built from flag-constrained repoRoot + fixed .regatta/items prefix + dir entry; same trust boundary as adapter/markdown.go
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

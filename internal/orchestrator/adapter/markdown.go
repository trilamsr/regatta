// Package adapter holds the concrete schemas.WorkItemSource
// implementations the orchestrator can run against. The skeleton
// ships markdown_catalog only; github_issues, jira, linear, and the
// custom-binary protocol land in follow-up commits.
package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// MarkdownCatalogConfig configures a file-backed adapter that reads
// work items from <root>/.regatta/items/*.md.
//
// File format:
//
//	---
//	id: ITEM-001
//	title: First item
//	lane: server
//	status: planned
//	dependencies: ITEM-000, ITEM-002
//	linked_artifact: docs/rfc/001.md
//	---
//
//	(free body text - ignored by the parser)
//
//	## Acceptance criteria
//
//	- [planned] c1: First criterion text
//	- [planned] c2: Second criterion text
//
// SourceRef.SHA is the sha256 of the file bytes; L0 verifies criterion
// text against the on-disk file at that SHA. Re-saving via
// UpdateStatus changes the SHA, which is expected: L0 anchors on the
// state-at-read, not state-at-mutate.
type MarkdownCatalogConfig struct {
	// Root is the repo root. The adapter looks under
	// <Root>/.regatta/items for *.md files.
	Root string

	// MinPoll is the minimum delay the orchestrator should wait
	// between successive List calls. Defaults to 5s.
	MinPoll time.Duration

	// Logger receives printf-style diagnostics for items skipped due
	// to parse errors. Nil is silent.
	Logger func(format string, args ...any)

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("adapter/markdown") which resolves
	// to the global provider — noop until obs/otel.Setup runs. Per W6
	// spec §3.3 + feedback_spec_pattern_authority.
	Tracer trace.Tracer
}

type markdownCatalog struct {
	cfg    MarkdownCatalogConfig
	logf   func(format string, args ...any)
	tracer trace.Tracer
	mu     sync.Mutex
}

// NewMarkdownCatalog returns a schemas.WorkItemSource backed by
// .regatta/items/*.md under cfg.Root.
func NewMarkdownCatalog(cfg MarkdownCatalogConfig) (schemas.WorkItemSource, error) {
	if cfg.Root == "" {
		return nil, errors.New("adapter: MarkdownCatalogConfig.Root is required")
	}
	abs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("adapter: resolve root: %w", err)
	}
	cfg.Root = abs
	if cfg.MinPoll <= 0 {
		cfg.MinPoll = 5 * time.Second
	}
	logf := cfg.Logger
	if logf == nil {
		logf = func(string, ...any) {}
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("adapter/markdown")
	}
	return &markdownCatalog{cfg: cfg, logf: logf, tracer: tracer}, nil
}

func (m *markdownCatalog) itemsDir() string {
	return filepath.Join(m.cfg.Root, ".regatta", "items")
}

// List walks the items directory and returns one WorkItem per *.md
// file. Errors on individual files surface as the function error; the
// orchestrator treats that as a transient adapter failure.
func (m *markdownCatalog) List(ctx context.Context) ([]schemas.WorkItem, error) {
	// W6 spec §8 T5: open a span at the main entry function so the
	// adapter's filesystem-scan latency shows up in the trace tree.
	ctx, span := m.tracer.Start(ctx, "adapter.markdown.list")
	defer span.End()
	m.mu.Lock()
	defer m.mu.Unlock()
	dir := m.itemsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("adapter: read %s: %w", dir, err)
	}
	var out []schemas.WorkItem
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		// "_" / "." prefixes are templates or drafts; skip.
		if strings.HasPrefix(entry.Name(), "_") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		item, err := m.readItem(path)
		if err != nil {
			return nil, fmt.Errorf("adapter: read %s: %w", path, err)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if err := checkAcyclic(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get loads a single work item by ID.
func (m *markdownCatalog) Get(ctx context.Context, id schemas.WorkItemID) (schemas.WorkItem, error) {
	items, err := m.List(ctx)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return schemas.WorkItem{}, fmt.Errorf("%w: %s", schemas.ErrNotFound, id)
}

// UpdateStatus rewrites the frontmatter status (and citation) for id.
// Idempotent: a no-op call leaves the file mtime untouched.
func (m *markdownCatalog) UpdateStatus(ctx context.Context, id schemas.WorkItemID, status schemas.Status, citation string) error {
	// Mirror the parse-time enum; widening here keeps the write path
	// from rejecting what List() already round-trips (issue #493).
	if !validStatus(status) {
		return fmt.Errorf("%w: %s", schemas.ErrInvalidStatus, status)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := m.findFileFor(id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("adapter: read %s: %w", path, err)
	}
	updated, changed := rewriteStatus(data, string(status), citation)
	if !changed {
		return nil
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("adapter: write %s: %w", path, err)
	}
	return nil
}

// Capabilities reports markdown_catalog's feature flags.
func (m *markdownCatalog) Capabilities() schemas.Capabilities {
	return schemas.Capabilities{
		Webhook:         false,
		BulkUpdate:      false,
		MinPollInterval: m.cfg.MinPoll,
		SupportedStatuses: []schemas.Status{
			schemas.StatusPlanned,
			schemas.StatusInProgress,
			schemas.StatusDone,
			schemas.StatusClosedResolved,
		},
	}
}

func (m *markdownCatalog) findFileFor(id schemas.WorkItemID) (string, error) {
	dir := m.itemsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("adapter: read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "_") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		item, err := m.readItem(path)
		if err != nil {
			m.logf("adapter: skip %s: %v", path, err)
			continue
		}
		if item.ID == id {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: %s", schemas.ErrNotFound, id)
}

func (m *markdownCatalog) readItem(path string) (schemas.WorkItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	parsed, err := parseMarkdownItem(data)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	rel, err := filepath.Rel(m.cfg.Root, path)
	if err != nil {
		rel = path
	}
	sum := sha256.Sum256(data)
	parsed.Source = schemas.SourceRef{
		Kind:    "file",
		Locator: filepath.ToSlash(rel),
		SHA:     "sha256:" + hex.EncodeToString(sum[:]),
	}
	return parsed, nil
}

func checkAcyclic(items []schemas.WorkItem) error {
	idx := make(map[schemas.WorkItemID]*schemas.WorkItem, len(items))
	for i := range items {
		idx[items[i].ID] = &items[i]
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[schemas.WorkItemID]int, len(items))
	var visit func(schemas.WorkItemID) error
	visit = func(id schemas.WorkItemID) error {
		switch color[id] {
		case gray:
			return fmt.Errorf("%w: at %s", schemas.ErrDependencyCycle, id)
		case black:
			return nil
		}
		color[id] = gray
		node, ok := idx[id]
		if ok {
			for _, dep := range node.Dependencies {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	for _, it := range items {
		if err := visit(it.ID); err != nil {
			return err
		}
	}
	return nil
}

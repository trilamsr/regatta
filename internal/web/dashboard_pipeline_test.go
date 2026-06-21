package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestLoadPipelineStageItems_QueuedDBError_Propagates asserts a closed-DB queued query returns error + nil slice (#MAY-75).
func TestLoadPipelineStageItems_QueuedDBError_Propagates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "queued-err.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	_ = db.Close()
	items, err := loadPipelineStageItems(context.Background(), Dependencies{DB: db, Clock: clock}, pipelineStageQueued)
	if err == nil {
		t.Fatalf("closed-DB queued query: want error, got nil")
	}
	if items != nil {
		t.Fatalf("closed-DB queued query: want nil items, got %v", items)
	}
}

// TestLoadPipelineStageItems_AgentSlugDBError_Propagates asserts a closed-DB non-queued slug propagates the agentsForPipelineSlug error (#MAY-75).
func TestLoadPipelineStageItems_AgentSlugDBError_Propagates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agent-err.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	_ = db.Close()
	items, err := loadPipelineStageItems(context.Background(), Dependencies{DB: db, Clock: clock}, pipelineStageRunning)
	if err == nil {
		t.Fatalf("closed-DB agent-slug query: want error, got nil")
	}
	if items != nil {
		t.Fatalf("closed-DB agent-slug query: want nil items, got %v", items)
	}
}

// TestServePipelineDrawer_DBError_RendersDegraded asserts a closed-DB drawer render shows the degraded block, not a silent empty list (#MAY-75).
func TestServePipelineDrawer_DBError_RendersDegraded(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "drawer-err.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	_ = db.Close()
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/pipeline/"+pipelineStageQueued, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("drawer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "no items at this stage") {
		t.Fatalf("closed-DB drawer rendered genuine-empty copy instead of degraded block: %s", body)
	}
	if !strings.Contains(body, "unavailable") {
		t.Fatalf("closed-DB drawer missing degraded marker: %s", body)
	}
}

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newEtagDashboardDB(t *testing.T) *state.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "etag.db")
	clock := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestDashboard_ETag304OnUnchanged asserts a second poll with the prior
// ETag returns 304 when the row-set is unchanged (R-MEGA-2 P2).
func TestDashboard_ETag304OnUnchanged(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	db := newEtagDashboardDB(t)
	clock := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/panels/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first poll status=%d body=%s", rec.Code, rec.Body.String())
	}
	tag := rec.Header().Get("ETag")
	if tag == "" {
		t.Fatalf("first poll missing ETag header")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ui/panels/agents", nil)
	req2.Header.Set("If-None-Match", tag)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("second poll status=%d; want 304", rec2.Code)
	}
	if got := rec2.Body.Len(); got != 0 {
		t.Fatalf("304 body length=%d; want 0", got)
	}
}

// TestDashboard_ETag200OnChanged asserts a row-set change flips the
// digest and serves a fresh 200 with a new ETag (R-MEGA-2 P2).
func TestDashboard_ETag200OnChanged(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	db := newEtagDashboardDB(t)
	clock := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	ctx := context.Background()

	if err := db.UpsertWorkItem(ctx, state.WorkItem{ID: "WI-1", Kind: state.KindFeature, Title: "first", Lane: "server", Status: state.WorkStatusPlanned}, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/panels/work-items", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first poll status=%d", rec.Code)
	}
	tag1 := rec.Header().Get("ETag")

	if err := db.UpsertWorkItem(ctx, state.WorkItem{ID: "WI-2", Kind: state.KindFeature, Title: "second", Lane: "server", Status: state.WorkStatusPlanned}, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("seed 2: %v", err)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ui/panels/work-items", nil)
	req2.Header.Set("If-None-Match", tag1)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("after change status=%d; want 200", rec2.Code)
	}
	tag2 := rec2.Header().Get("ETag")
	if tag2 == "" || tag2 == tag1 {
		t.Fatalf("ETag did not change after row-set delta: tag1=%q tag2=%q", tag1, tag2)
	}
}

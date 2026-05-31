package reaper

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// captureHandler is a minimal slog.Handler that records every Record
// so tests can assert which events were emitted. Lives here rather
// than in internal/obstest because Task D ships before the shared
// helper package exists — the shape is small enough to inline.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler     { return h }

func (h *captureHandler) findEvent(name obs.EventName) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == string(name) {
			return r, true
		}
	}
	return slog.Record{}, false
}

func TestReaper_EmitsKilledOnReap(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	wm := newWM(t)
	killer := &fakeKiller{}
	h := &captureHandler{}
	r := New(Config{
		DB:     db,
		WM:     wm,
		Killer: killer,
		Logger: slog.New(h),
	})

	a := upsert(t, db, "WORK-1", "server")
	if _, err := wm.Create(ctx, a.ID, "HEAD"); err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	driveToDone(t, db, a.ID)

	if err := r.Reap(ctx, a.ID); err != nil {
		t.Fatalf("reap: %v", err)
	}

	if _, ok := h.findEvent(obs.EventReapKilled); !ok {
		t.Fatalf("reap.killed event not emitted; records=%+v", h.records)
	}
}

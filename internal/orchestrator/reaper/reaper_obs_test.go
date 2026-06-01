package reaper

import (
	"context"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

func TestReaper_EmitsKilledOnReap(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	wm := newWM(t)
	killer := &fakeKiller{}
	h := obstest.New()
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

	if _, ok := h.FindEvent(obs.EventReapKilled); !ok {
		t.Fatalf("reap.killed event not emitted; records=%+v", h.Records())
	}
}

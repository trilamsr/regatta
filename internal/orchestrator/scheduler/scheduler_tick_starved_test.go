package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestScheduler_TickEmitsStarvedEvent_WhenSpawnableButLaneCapped asserts the scheduler emits obs.EventSchedulerTickStarved at INFO when spawnable>0 yet reserved==0 — the #1218 stall signal hidden behind tickLogLevel=DEBUG.
func TestScheduler_TickEmitsStarvedEvent_WhenSpawnableButLaneCapped(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		seedPlanned(t, db, fmt.Sprintf("WORK-STARVED-%d", i), "server")
	}

	h := obstest.New()
	sch := New(db, Config{
		LockTTL:     time.Minute,
		ParallelCap: 2,
		Logger:      slog.New(h),
	})

	// Tick 1 fills the cap (2 reservations).
	ids1, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if len(ids1) != 2 {
		t.Fatalf("tick1 reserved=%d, want 2", len(ids1))
	}

	// Tick 2: cap saturated, 2 spawnable items waiting → starved.
	ids2, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if len(ids2) != 0 {
		t.Fatalf("tick2 reserved=%d, want 0", len(ids2))
	}

	var starved *struct {
		evaluated int64
		reserved  int64
		reason    string
	}
	for _, r := range h.Records() {
		if r.Message == string(obs.EventSchedulerTickStarved) {
			s := struct {
				evaluated int64
				reserved  int64
				reason    string
			}{}
			r.Attrs(func(a slog.Attr) bool {
				switch a.Key {
				case string(obs.KeyWorkItemsEvaluated):
					s.evaluated = a.Value.Int64()
				case string(obs.KeyAgentsReserved):
					s.reserved = a.Value.Int64()
				case string(obs.KeyReason):
					s.reason = a.Value.String()
				}
				return true
			})
			starved = &s
			break
		}
	}
	if starved == nil {
		records := h.Records()
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, r.Message)
		}
		t.Fatalf("expected %q record; got %s", string(obs.EventSchedulerTickStarved), strings.Join(msgs, ", "))
	}
	if starved.evaluated == 0 {
		t.Fatalf("evaluated_count=0; want >0")
	}
	if starved.reserved != 0 {
		t.Fatalf("agents_reserved=%d; want 0", starved.reserved)
	}
	if starved.reason != "lane_saturated" {
		t.Fatalf("reason=%q; want lane_saturated", starved.reason)
	}
}

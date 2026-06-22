package orchestrator

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
)

// TestScheduleOnce_EmitsTickCompletedSubstrateEvent asserts the tick loop records tick.completed to the events table so `regatta status` reads a fresh liveness signal even on an idle daemon (R30).
func TestScheduleOnce_EmitsTickCompletedSubstrateEvent(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 0)

	before, err := db.ListEvents(ctx, 1000)
	if err != nil {
		t.Fatalf("ListEvents before: %v", err)
	}
	beforeTickCount := 0
	for _, e := range before {
		if e.Kind == string(obs.EventTickCompleted) {
			beforeTickCount++
		}
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	after, err := db.ListEvents(ctx, 1000)
	if err != nil {
		t.Fatalf("ListEvents after: %v", err)
	}
	afterTickCount := 0
	for _, e := range after {
		if e.Kind == string(obs.EventTickCompleted) {
			afterTickCount++
		}
	}

	if got, want := afterTickCount-beforeTickCount, 1; got != want {
		t.Fatalf("tick.completed substrate events added=%d; want %d (daemon liveness signal must reach sibling-CLI via events table)", got, want)
	}
}

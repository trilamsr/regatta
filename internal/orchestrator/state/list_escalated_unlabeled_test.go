package state

import (
	"context"
	"fmt"
	"testing"
)

// TestListEscalatedUnlabeled_FiltersAlreadyLabeledAndCapsByLimit pins the absence-of-event filter that bounds rejectionrouter.sweepUnlabeled per tick.
func TestListEscalatedUnlabeled_FiltersAlreadyLabeledAndCapsByLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// 5 escalated agents — 3 already labeled, 2 unlabeled.
	const totalEscalated = 5
	const wantUnlabeled = 2
	unlabeled := make(map[int64]struct{}, wantUnlabeled)
	for i := 0; i < totalEscalated; i++ {
		id := newEscalatedAgent(t, db, fmt.Sprintf("WI-ESC-%d", i))
		if i < wantUnlabeled {
			unlabeled[id] = struct{}{}
			continue
		}
		if err := db.RecordEvent(ctx, id, "labeled", `{"label":"needs-human"}`); err != nil {
			t.Fatalf("record labeled event: %v", err)
		}
	}
	// Control row: a pending agent must never surface in an escalated-only query.
	if _, err := db.UpsertPending(ctx, "WI-PENDING", "server"); err != nil {
		t.Fatalf("upsert pending control: %v", err)
	}

	got, err := db.ListEscalatedUnlabeled(ctx, 100)
	if err != nil {
		t.Fatalf("ListEscalatedUnlabeled: %v", err)
	}
	if len(got) != wantUnlabeled {
		t.Fatalf("got %d unlabeled, want %d", len(got), wantUnlabeled)
	}
	for _, a := range got {
		if _, ok := unlabeled[a.ID]; !ok {
			t.Errorf("agent %d returned but is already labeled", a.ID)
		}
		if a.State != AgentEscalated {
			t.Errorf("agent %d state=%q; want escalated", a.ID, a.State)
		}
	}

	// Limit clamp: ask for 1, get 1 even though 2 qualify.
	clamped, err := db.ListEscalatedUnlabeled(ctx, 1)
	if err != nil {
		t.Fatalf("ListEscalatedUnlabeled(limit=1): %v", err)
	}
	if len(clamped) != 1 {
		t.Fatalf("limit=1 returned %d rows, want 1", len(clamped))
	}
}

// TestListEscalatedUnlabeled_PerTickCostIndependentOfLabeledBacklog asserts the row count returned stays at the limit (or 0) regardless of how many already-labeled escalated rows accumulate.
func TestListEscalatedUnlabeled_PerTickCostIndependentOfLabeledBacklog(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Seed an ever-growing pile of escalated+labeled agents — the
	// failure mode #478 describes (terminal state, never shrinks).
	const backlog = 50
	for i := 0; i < backlog; i++ {
		id := newEscalatedAgent(t, db, fmt.Sprintf("WI-OLD-%d", i))
		if err := db.RecordEvent(ctx, id, "labeled", `{"label":"needs-human"}`); err != nil {
			t.Fatalf("record labeled: %v", err)
		}
	}

	// Single unlabeled candidate — the only row the sweep should care about.
	want := newEscalatedAgent(t, db, "WI-NEW")

	got, err := db.ListEscalatedUnlabeled(ctx, 100)
	if err != nil {
		t.Fatalf("ListEscalatedUnlabeled: %v", err)
	}
	if len(got) != 1 || got[0].ID != want {
		t.Fatalf("got=%+v; want exactly one row with id=%d (backlog must be filtered at SQL)", got, want)
	}
}

// newEscalatedAgent drives an agent through pending->...->escalated for use by the labeled-event filter tests.
func newEscalatedAgent(t *testing.T, db *DB, workItemID string) int64 {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "server")
	if err != nil {
		t.Fatalf("upsert %s: %v", workItemID, err)
	}
	sha := "deadbeef"
	mut := AgentMutation{PRSHA: &sha}
	for _, next := range []AgentState{AgentSpawning, AgentRunning, AgentPROpen, AgentGatesRunning, AgentGatesFailed} {
		m := AgentMutation{}
		if next == AgentPROpen {
			m = mut
		}
		if _, err := db.TransitionAgent(ctx, a.ID, next, m); err != nil {
			t.Fatalf("%s: transition -> %s: %v", workItemID, next, err)
		}
	}
	if _, err := db.TransitionAgent(ctx, a.ID, AgentEscalated, AgentMutation{}); err != nil {
		t.Fatalf("%s: transition -> escalated: %v", workItemID, err)
	}
	return a.ID
}

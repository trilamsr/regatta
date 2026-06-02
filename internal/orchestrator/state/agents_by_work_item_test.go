package state

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestGetAgentByWorkItemID_ReturnsExistingAgent confirms the helper round-trips a row inserted via UpsertPending.
func TestGetAgentByWorkItemID_ReturnsExistingAgent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	want, err := db.UpsertPending(ctx, "WI-LOOKUP", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := db.GetAgentByWorkItemID(ctx, "WI-LOOKUP")
	if err != nil {
		t.Fatalf("GetAgentByWorkItemID: %v", err)
	}
	if got.ID != want.ID || got.WorkItemID != "WI-LOOKUP" || got.Lane != "server" {
		t.Fatalf("got=%+v; want id=%d wi=WI-LOOKUP lane=server", got, want.ID)
	}
}

// TestGetAgentByWorkItemID_MissingReturnsErrNoRows pins the contract scheduler.applyL4Gate depends on to skip emit-with-agent_id when no row exists yet.
func TestGetAgentByWorkItemID_MissingReturnsErrNoRows(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.GetAgentByWorkItemID(ctx, "WI-MISSING")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err=%v; want sql.ErrNoRows", err)
	}
}

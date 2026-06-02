package spend_test

import (
	"context"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestSpawnerCallback_WritesRowWithRequestScope — substrate row carries distinct OperatorID/DAGID/WorkItemID/RunID supplied on Request (no wave-2 shortcut collapse).
func TestSpawnerCallback_WritesRowWithRequestScope(t *testing.T) {
	db := openWriterDB(t)
	factory := spend.SpawnerCallback(db,
		spend.WriteOptions{Key: testKey, KeyID: testKeyID},
		spend.CallScope{WrittenBy: "test-claude-spawner"})
	cb := factory(spawner.Request{
		AgentID:    7,
		WorkItemID: "WI-42",
		Lane:       "server",
		OperatorID: "agent-7",
		DAGID:      "PROG-X",
		RunID:      "run-42",
	})

	err := cb(context.Background(), spawner.StreamResultEvent{
		MessageID: "msg_01abc", Model: "claude-sonnet-4-7",
		InputTokens: 120, OutputTokens: 42,
	})
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	var payload, runID string
	if err := db.QueryRow(`SELECT payload_json, run_id FROM substrate_events WHERE kind='token_spend'`).Scan(&payload, &runID); err != nil {
		t.Fatalf("read row: %v", err)
	}
	for _, want := range []string{
		`"work_item_id":"WI-42"`,
		`"operator_id":"agent-7"`,
		`"dag_id":"PROG-X"`,
		`"call_id":"msg_01abc"`,
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q: %s", want, payload)
		}
	}
	if runID != "run-42" {
		t.Fatalf("run_id: got %q want %q", runID, "run-42")
	}
}

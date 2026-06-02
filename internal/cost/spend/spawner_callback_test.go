package spend_test

import (
	"context"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestSpawnerCallback_WritesRowWithRequestScope — one fake result event yields one substrate row stamped with the Request's WorkItemID + Lane.
func TestSpawnerCallback_WritesRowWithRequestScope(t *testing.T) {
	db := openWriterDB(t)
	factory := spend.SpawnerCallback(db,
		spend.WriteOptions{Key: testKey, KeyID: testKeyID},
		spend.CallScope{WrittenBy: "test-claude-spawner"})
	cb := factory(spawner.Request{AgentID: 7, WorkItemID: "WI-42", Lane: "server"})

	err := cb(context.Background(), spawner.StreamResultEvent{
		MessageID: "msg_01abc", Model: "claude-sonnet-4-7",
		InputTokens: 120, OutputTokens: 42,
	})
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	var raw string
	if err := db.QueryRow(`SELECT payload_json FROM substrate_events WHERE kind='token_spend'`).Scan(&raw); err != nil {
		t.Fatalf("read row: %v", err)
	}
	for _, want := range []string{`"work_item_id":"WI-42"`, `"operator_id":"server"`, `"call_id":"msg_01abc"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("payload missing %q: %s", want, raw)
		}
	}
}

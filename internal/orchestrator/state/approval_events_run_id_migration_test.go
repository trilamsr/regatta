package state

import (
	"testing"
)

// TestApprovalEventsRunIDMigration_RoundTrip pins migration 0020 lets approval_events carry a non-empty run_id (#operator-console-S0).
func TestApprovalEventsRunIDMigration_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	if _, err := db.SQL().Exec(`
		INSERT INTO work_items (id, kind, title, lane, status, source, last_seen_at, created_at, updated_at)
		VALUES ('wi-1', 'agent', 't', 'lane-a', 'pending', 'test', 0, 0, 0)
	`); err != nil {
		t.Fatalf("seed wi: %v", err)
	}
	if _, err := db.SQL().Exec(`
		INSERT INTO approvals (id, work_item_id, gate_name, requested_at, requested_by,
		                       reviewer_set_snapshot_json, timeout_at, created_at, updated_at)
		VALUES ('ap-1', 'wi-1', 'g1', 0, 'tester', '[]', 9999999999, 0, 0)
	`); err != nil {
		t.Fatalf("seed approval: %v", err)
	}
	_, err := db.SQL().Exec(`
		INSERT INTO approval_events (
			approval_id, ts, kind, actor, payload_json, token_jti, run_id
		) VALUES ('ap-1', 1717000000, 'decided', 'rev-1', '{}', 'jti-1', 'run-1')
	`)
	if err != nil {
		t.Fatalf("insert approval_events w/ run_id: %v", err)
	}
	var got string
	if err := db.SQL().QueryRow(`SELECT run_id FROM approval_events WHERE approval_id='ap-1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "run-1" {
		t.Errorf("got run_id=%q want run-1", got)
	}
}

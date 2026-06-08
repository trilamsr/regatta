package state

import (
	"testing"
)

// TestWorkItemsRunIDMigration_RoundTrip pins migration 0019 lets work_items carry a non-empty run_id (#operator-console-S0).
func TestWorkItemsRunIDMigration_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.SQL().Exec(`
		INSERT INTO work_items (id, kind, title, lane, status, source,
		                         last_seen_at, created_at, updated_at, run_id)
		VALUES ('wi-1', 'agent', 't', 'lane-a', 'pending', 'test',
		         0, 0, 0, 'run-1')
	`)
	if err != nil {
		t.Fatalf("insert work_items w/ run_id: %v", err)
	}
	var got string
	if err := db.SQL().QueryRow(`SELECT run_id FROM work_items WHERE id='wi-1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "run-1" {
		t.Errorf("got run_id=%q want run-1", got)
	}
}

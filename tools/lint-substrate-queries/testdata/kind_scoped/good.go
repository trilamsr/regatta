package good

import "database/sql"

// GoodQuery has both run_id=? and kind=? scoping; lint passes.
func GoodQuery(db *sql.DB) error {
	_, err := db.Query(
		`SELECT id FROM substrate_events WHERE run_id = ? AND kind = ?`,
		"run-1", "node_output",
	)
	return err
}

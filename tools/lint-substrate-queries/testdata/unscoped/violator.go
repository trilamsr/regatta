package violator

import "database/sql"

// BadQuery selects from substrate_events with no kind filter.
// Lint must reject.
func BadQuery(db *sql.DB) error {
	_, err := db.Query(`SELECT * FROM substrate_events WHERE run_id = ?`, "run-1")
	return err
}

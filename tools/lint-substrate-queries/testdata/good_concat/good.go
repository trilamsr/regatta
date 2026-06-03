package good

import "database/sql"

// GoodConcat builds the SQL via "+" concat but the union of operands
// carries `kind = ?` and `run_id = ?`. Lint passes (#234).
func GoodConcat(db *sql.DB) error {
	q := "SELECT id FROM " + "substrate_events" + " WHERE run_id = ? AND " + "kind = ?"
	_, err := db.Query(q, "run-1", "node_output")
	return err
}

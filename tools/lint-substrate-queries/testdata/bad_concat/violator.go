package violator

import "database/sql"

const tableName = "substrate_events"

// BadConcat builds the SQL via "+" concat; the union of operands references
// substrate_events but lacks `kind = ?`. Lint must reject (#234).
func BadConcat(db *sql.DB, scope string) error {
	q := "SELECT * FROM " + tableName + " WHERE " + scope
	_, err := db.Query(q)
	return err
}

package violator

import "database/sql"

// CrossRunBad has kind=? but no run_id=? and no tenant_id=?.
// Cross-run reads (audit, billing) must filter by tenant_id.
func CrossRunBad(db *sql.DB) error {
	_, err := db.Query(`SELECT id FROM substrate_events WHERE kind = ?`, "token_spend")
	return err
}

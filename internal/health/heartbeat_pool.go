package health

import (
	"database/sql"
	"errors"
)

// ConfigureHeartbeatPool pins db to the single-connection pool spec §3.5
// fix #7 requires so stalls in the main pool can never starve the
// heartbeat writer. Composition root owns the sql.Open; this function
// only sets pool limits on the injected handle.
func ConfigureHeartbeatPool(db *sql.DB) error {
	if db == nil {
		return errors.New("health: ConfigureHeartbeatPool requires non-nil db")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)
	return nil
}

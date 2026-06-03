package health

import (
	"database/sql"
)

// OpenHeartbeatPool returns a dedicated *sql.DB whose pool is pinned to
// one connection — spec §3.5 fix #7. Stalls in the main pool can never
// starve the heartbeat writer; staleness then honestly signals wedge.
//
// Caller owns Close; the pool is read in the writer goroutine only.
func OpenHeartbeatPool(driver, dsn string) (*sql.DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)
	return db, nil
}

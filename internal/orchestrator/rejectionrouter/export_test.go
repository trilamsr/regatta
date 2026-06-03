package rejectionrouter

import "database/sql"

// SetTxHook installs a fault-injection hook that fires inside the
// escalation tx between the gates_failed write and the escalated
// write. Used by tests to assert the atomic-rollback contract for
// issue #477; production callers never set it.
func SetTxHook(r *Router, hook func(*sql.Tx) error) {
	r.txHook = hook
}

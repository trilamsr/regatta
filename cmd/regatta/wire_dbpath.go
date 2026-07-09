package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// ensureDBParent creates the parent directory of the sqlite DB path
// before state.Open runs — sqlite's "unable to open database file" is
// cryptic and doesn't name the missing dir, so operators land on a
// stalled boot with no clue which mount is wrong. MkdirAll (not Mkdir)
// makes nested parents (e.g. /var/lib/regatta/data) work on first boot;
// existing dirs are a no-op. Empty dbPath is a caller bug — the
// composition root already validates that flag.
func ensureDBParent(dbPath string) error {
	if dbPath == "" {
		return fmt.Errorf("db path is empty")
	}
	parent := filepath.Dir(dbPath)
	if parent == "" || parent == "." {
		return nil
	}
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create parent %q: %w", parent, err)
	}
	return nil
}

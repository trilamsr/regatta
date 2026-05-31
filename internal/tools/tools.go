//go:build tools

// Package tools pins dependencies not yet imported by production
// code so `go mod tidy` doesn't prune them. The tools build tag is
// the Go-community convention for files kept solely for this
// purpose; nothing under this tag compiles into the regatta binary.
//
//   - github.com/gofrs/flock — process-level lockfile wrapper
//   - pgregory.net/rapid     — property-based testing library
package tools

import (
	_ "github.com/gofrs/flock"
	_ "pgregory.net/rapid"
)

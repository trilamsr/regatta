//go:build tools

// Package tools pins MVP-1 dependencies that haven't been imported
// by production code yet. Task A1 of the planner-as-DAG series
// front-loads every dep the series will need so later tasks never
// touch go.mod/go.sum. Each blank import below is consumed by a
// future task:
//
//   - github.com/gofrs/flock — Task A2 (process-level lockfile)
//   - pgregory.net/rapid     — Task A4 (property tests for cycle detection)
//
// The tools build tag is the Go-community convention for files kept
// around solely to defeat `go mod tidy`'s prune-unused-modules pass.
package tools

import (
	_ "github.com/gofrs/flock"
	_ "pgregory.net/rapid"
)

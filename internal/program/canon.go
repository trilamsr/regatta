// Package program — canonical JSON for the outputs journal.
//
// Determinism boundary: every byte landing in work_item_outputs.output_json
// passes through canonicaliseJSON. Replay invariant (spec §4) depends on
// the canonical form being byte-stable across orchestrator versions.
// Implementation lives in internal/canon so package state can share it
// without violating spec decision 18 (one-way program→state import).
package program

import "github.com/trilamsr/regatta/internal/canon"

// canonicaliseJSON delegates to internal/canon.CanonicaliseJSON. The
// indirection preserves the in-package symbol name adversarial tests
// (canon_adversarial_test.go) and brief_loader call sites grep for,
// while the canonical implementation lives in a leaf package both
// program and state can import.
func canonicaliseJSON(payload []byte) ([]byte, error) {
	return canon.CanonicaliseJSON(payload)
}

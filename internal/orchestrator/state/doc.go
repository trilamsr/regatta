// Package state is split via the Option E hybrid pattern (plan #795,
// docs/engineer/specs/2026-06-04-state-package-split-design.md §4): the
// *DB receiver type and all 70+ receiver methods stay in this root package
// so the single-connection pool + BEGIN-IMMEDIATE tx + constructor-bound
// clock invariants survive; pure data types and free functions peel into
// one-way subpackages:
//
//   - jsonscan/         pure JSON-array scanner ([]byte → []string)
//   - edgeagg/          pure edge-aggregation helpers + EdgeRow alias source
//   - transitions/      agent + work-item edge tables (pure data)
//   - cycle/            pure DFS cycle check over an adjacency map
//   - approvals_shadow/ divergence classifier + shadow-write config
//
// The tier order is strict and one-way: subpackages MUST NOT import state.
// scripts/check-state-tier-order.sh enforces this in `make check` so a
// reverse import (which would silently re-create the god-package via type
// aliases in state/aliases.go) fails the build with a readable message
// before `go build` would surface it as an opaque cycle.
//
// New helpers that fit the pure-data + free-function shape belong in a
// subpackage; new helpers that need *DB or per-instance state stay here.
package state

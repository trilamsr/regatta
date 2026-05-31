// Package orchestrator hosts the typed error sentinels shared across
// the MVP-1 universal-queue pipeline. Downstream packages
// (adaptersync, brief loader, lockfile, scheduler, state migrations)
// MUST import sentinels from here rather than calling errors.New
// at boundary points — verified by `make ci-check` grep gate.
//
// per RFC-0001 §3: cascade-soft, fail-fast PollOnce, single-source-of-truth
// work_items table.
package orchestrator

import "errors"

var (
	// ErrBriefSHAMismatch fires when the operator-pinned planner
	// prompt SHA in regatta.yaml does not match the on-disk prompt.
	ErrBriefSHAMismatch = errors.New("orchestrator: planner prompt SHA does not match pinned value")

	// ErrHMACInvalid fires when a program_brief.json fails HMAC
	// verification against the configured keyring.
	ErrHMACInvalid = errors.New("orchestrator: brief HMAC signature invalid")

	// ErrTargetExists fires when `regatta program plan --write` would
	// overwrite an existing brief with different content (use --force
	// to override).
	ErrTargetExists = errors.New("orchestrator: target file exists with different content")

	// ErrFlockHeld fires when the process-level lockfile is held by
	// another live regatta instance. Distinct from state.ErrLockHeld
	// which guards hotspot-locks within a single process.
	ErrFlockHeld = errors.New("orchestrator: process flock held by another instance")

	// ErrSchemaTooNew fires when a v2 database is opened by a binary
	// that only knows v1 migrations — downgrade-resistance.
	ErrSchemaTooNew = errors.New("orchestrator: database schema is newer than this binary supports")

	// ErrCycleDetected fires when work_items.depends_on_features would
	// introduce a cycle, blocking the upsert.
	ErrCycleDetected = errors.New("orchestrator: dependency cycle detected in work_items")
)

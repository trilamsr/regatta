// Package orchestrator hosts the typed error sentinels shared across
// the MVP-1 universal-queue pipeline. Downstream packages MUST import
// sentinels from here rather than calling errors.New at boundary
// points.
package orchestrator

import "errors"

var (
	// ErrBriefSHAMismatch fires when the operator-pinned planner
	// prompt SHA in regatta.yaml does not match the on-disk prompt.
	ErrBriefSHAMismatch = errors.New("orchestrator: planner prompt SHA does not match pinned value")

	// ErrPlannerPromptMissing fires when prompts.planner_path points
	// at a file that does not exist AND prompts.planner_sha is pinned.
	// A pinned hash with a missing file must fail closed -- silently
	// swapping in the embedded default defeats the pin.
	ErrPlannerPromptMissing = errors.New("orchestrator: planner prompt path missing while SHA is pinned")

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
)

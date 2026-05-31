// Package orchestrator hosts the typed error sentinels shared across
// the MVP-1 universal-queue pipeline. Downstream packages MUST import
// sentinels from here rather than calling errors.New at boundary
// points.
package orchestrator

import (
	"errors"

	"github.com/trilamsr/regatta/internal/orchestrator/lockfile"
)

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

	// ErrFlockHeld is re-exported from package lockfile to preserve the
	// orchestrator.ErrFlockHeld import path used across the codebase.
	// Canonical definition lives next to lockfile.Acquire because
	// PollOnce imports lockfile (one-way edge — flipping it produced
	// an import cycle).
	ErrFlockHeld = lockfile.ErrFlockHeld

	// ErrCascadeNonConverging fires when BriefLoader's dependency-
	// archive fixed-point loop exceeds its iteration cap. Indicates
	// corrupt depends_on_features graph data (e.g. a cycle that
	// bypassed CycleCheck) — operators page on this.
	ErrCascadeNonConverging = errors.New("orchestrator: dependency-archive cascade did not converge")
)

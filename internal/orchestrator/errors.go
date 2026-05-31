// Package orchestrator hosts the typed error sentinels shared across
// the MVP-1 universal-queue pipeline. Downstream packages MUST import
// sentinels from here rather than calling errors.New at boundary
// points.
package orchestrator

import (
	"errors"

	"github.com/google/cel-go/cel"

	"github.com/trilamsr/regatta/internal/orchestrator/lockfile"
)

// PredicateEnv aliases cel.Env so callers in the conditional-DAG
// evaluator (Wave 3) and the planner_v2 type-checker (Wave 2)
// share one symbol. Declared in W0-A so go.mod pins cel-go as a
// direct dep before any evaluator code lands.
type PredicateEnv = cel.Env

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

	// ErrPredicateCompile fires when a CEL predicate fails to compile
	// against the upstream feature's outputs_schema environment.
	// Caught at LoadAndVerifyBrief time so operators never see a
	// predicate failure mid-run.
	ErrPredicateCompile = errors.New("orchestrator: CEL predicate failed to compile")

	// ErrPredicateUnknownField fires when a CEL predicate references
	// an `out.<field>` path absent from the upstream's outputs_schema.
	ErrPredicateUnknownField = errors.New("orchestrator: CEL predicate references field absent from outputs_schema")

	// ErrPredicateTypeMismatch fires when CEL's type checker rejects
	// the predicate against the env derived from outputs_schema.
	ErrPredicateTypeMismatch = errors.New("orchestrator: CEL predicate has type mismatch against outputs_schema")

	// ErrEdgeMissingDefault fires when a feature has ≥1 outgoing
	// predicated edge but no DefaultNext target.
	ErrEdgeMissingDefault = errors.New("orchestrator: conditional fan-out missing default_next")

	// ErrEdgeUnknownTarget fires when an edge references a feature ID
	// not present in the brief.
	ErrEdgeUnknownTarget = errors.New("orchestrator: edge references unknown feature id")

	// ErrJournalNotFound fires when edge evaluation cannot locate an
	// outputs journal entry for the upstream work_item.
	ErrJournalNotFound = errors.New("orchestrator: outputs journal entry not found for work_item")
)

// Package orchestrator hosts the typed error sentinels shared across
// the MVP-1 universal-queue pipeline. Downstream packages MUST import
// from here rather than errors.New at boundary points.
package orchestrator

import (
	"errors"

	"github.com/google/cel-go/cel"

	"github.com/trilamsr/regatta/internal/orchestrator/lockfile"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// PredicateEnv aliases cel.Env so the conditional-DAG evaluator and
// planner_v2 type-checker share one symbol.
type PredicateEnv = cel.Env

var (
	// ErrBriefSHAMismatch fires when the operator-pinned planner
	// prompt SHA in regatta.yaml does not match the on-disk prompt.
	ErrBriefSHAMismatch = errors.New("orchestrator: planner prompt SHA does not match pinned value")

	// ErrPlannerPromptMissing fires when prompts.planner_path is missing
	// while prompts.planner_sha is pinned. Fail-closed: a missing file
	// must not silently fall back to the embedded default.
	ErrPlannerPromptMissing = errors.New("orchestrator: planner prompt path missing while SHA is pinned")

	// ErrHMACInvalid fires when a program_brief.json fails HMAC
	// verification against the configured keyring.
	ErrHMACInvalid = errors.New("orchestrator: brief HMAC signature invalid")

	// ErrTargetExists fires when `regatta program plan --write` would
	// overwrite an existing brief with different content (use --force
	// to override).
	ErrTargetExists = errors.New("orchestrator: target file exists with different content")

	// ErrFlockHeld re-exports lockfile.ErrFlockHeld so callers keep the
	// orchestrator.ErrFlockHeld path; canonical def lives in lockfile
	// (one-way import edge to avoid a cycle).
	ErrFlockHeld = lockfile.ErrFlockHeld

	// ErrCascadeNonConverging fires when the dependency-archive
	// fixed-point exceeds its iteration cap. Indicates a corrupt
	// depends_on_features graph; pageable.
	ErrCascadeNonConverging = errors.New("orchestrator: dependency-archive cascade did not converge")

	// ErrPredicateCompile fires when a CEL predicate fails to compile
	// against the upstream feature's outputs_schema env. Caught at
	// LoadAndVerifyBrief time, not mid-run.
	ErrPredicateCompile = errors.New("orchestrator: CEL predicate failed to compile")

	// ErrPredicateUnknownField fires when a CEL predicate references
	// an `out.<field>` path absent from the upstream's outputs_schema.
	ErrPredicateUnknownField = errors.New("orchestrator: CEL predicate references field absent from outputs_schema")

	// ErrPredicateTypeMismatch fires when CEL's type checker rejects
	// the predicate against the env derived from outputs_schema.
	ErrPredicateTypeMismatch = errors.New("orchestrator: CEL predicate has type mismatch against outputs_schema")

	// ErrPredicateEval fires when a compiled CEL predicate fails at
	// runtime against a journaled output. Distinct from
	// ErrPredicateCompile so alerts separate config bugs from
	// journal-shape mismatches.
	ErrPredicateEval = errors.New("orchestrator: CEL predicate failed at evaluation")

	// ErrEdgeMissingDefault fires when a feature has ≥1 outgoing
	// predicated edge but no DefaultNext target.
	ErrEdgeMissingDefault = errors.New("orchestrator: conditional fan-out missing default_next")

	// ErrEdgeUnknownTarget fires when an edge references a feature ID
	// not present in the brief.
	ErrEdgeUnknownTarget = errors.New("orchestrator: edge references unknown feature id")

	// ErrEdgeUnreachable fires when DefaultNext is not reachable via
	// the forward closure of outgoing edges (spec §3.3 rule 2c): an
	// authoring deadlock where all predicates fail and the default
	// routes to an orphan.
	ErrEdgeUnreachable = errors.New("orchestrator: default_next target not reachable from source")

	// ErrJournalNotFound re-exports state.ErrJournalNotFound; canonical
	// def lives in state to avoid an orchestrator→state→orchestrator cycle.
	ErrJournalNotFound = state.ErrJournalNotFound
)

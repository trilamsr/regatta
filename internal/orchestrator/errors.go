// Package orchestrator hosts typed error sentinels shared across the
// MVP-1 universal-queue pipeline. Downstream packages MUST import from
// here rather than errors.New at boundary points.
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

// Sentinels signaling brief / planner / HMAC failures.
var (
	// ErrBriefSHAMismatch — operator-pinned planner SHA disagrees with the on-disk prompt.
	ErrBriefSHAMismatch = errors.New("orchestrator: planner prompt SHA does not match pinned value")
	// ErrPlannerPromptMissing fails closed when planner_path is missing while planner_sha is pinned.
	ErrPlannerPromptMissing = errors.New("orchestrator: planner prompt path missing while SHA is pinned")
	// ErrHMACInvalid — program_brief.json failed HMAC verification against the configured keyring.
	ErrHMACInvalid = errors.New("orchestrator: brief HMAC signature invalid")
	// ErrTargetExists — `regatta program plan --write` would overwrite divergent content.
	ErrTargetExists = errors.New("orchestrator: target file exists with different content")
	// ErrFlockHeld re-exports lockfile.ErrFlockHeld (canonical def lives in lockfile to avoid a cycle).
	ErrFlockHeld = lockfile.ErrFlockHeld
	// ErrCascadeNonConverging signals a corrupt depends_on_features graph — pageable, not retryable.
	ErrCascadeNonConverging = errors.New("orchestrator: dependency-archive cascade did not converge")
)

// CEL predicate sentinels. Compile errors fire at LoadAndVerifyBrief
// time; Eval errors fire mid-run — alerts split on them so config bugs
// stay distinguishable from journal-shape mismatches.
var (
	// ErrPredicateCompile — CEL predicate failed to compile.
	ErrPredicateCompile = errors.New("orchestrator: CEL predicate failed to compile")
	// ErrPredicateUnknownField — predicate references a field absent from outputs_schema.
	ErrPredicateUnknownField = errors.New("orchestrator: CEL predicate references field absent from outputs_schema")
	// ErrPredicateTypeMismatch — type-checker rejected the predicate.
	ErrPredicateTypeMismatch = errors.New("orchestrator: CEL predicate has type mismatch against outputs_schema")
	// ErrPredicateEval — compiled predicate failed at runtime.
	ErrPredicateEval = errors.New("orchestrator: CEL predicate failed at evaluation")
)

// Edge / journal sentinels.
var (
	// ErrEdgeMissingDefault — conditional fan-out missing default_next.
	ErrEdgeMissingDefault = errors.New("orchestrator: conditional fan-out missing default_next")
	// ErrEdgeUnknownTarget — edge references a feature id not in the brief.
	ErrEdgeUnknownTarget = errors.New("orchestrator: edge references unknown feature id")
	// ErrEdgeUnreachable surfaces an authoring deadlock — all predicates fail and default routes to an orphan.
	ErrEdgeUnreachable = errors.New("orchestrator: default_next target not reachable from source")
	// ErrJournalNotFound re-exports state.ErrJournalNotFound (canonical in state, avoids cycle).
	ErrJournalNotFound = state.ErrJournalNotFound
)

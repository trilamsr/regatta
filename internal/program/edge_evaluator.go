// EdgeEvaluator runs compiled CEL predicates against journaled output
// JSON. One per BriefLoader.Sync; lives in-process. Thread-safe.
//
// Cache key is (programID, edgeID): re-plans that mutate predicate text
// for the same edge ID are handled by InvalidateProgram, called from
// BriefLoader when a brief is removed or replaced. The cache is a
// sync.Map so the scheduler tick reads concurrently without contending
// against unrelated edge compiles.
//
// NO LLM client. Trap P1 — the deterministic predicate gate must never
// fan out to a model. A grep gate on this file in `make ci-check`
// enforces zero model-client imports.

package program

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// EdgeEvaluator caches one cel.Program per (programID, edgeID).
type EdgeEvaluator struct {
	cache    sync.Map // edgeCacheKey -> *cachedProgram
	compiles atomic.Int64
}

type edgeCacheKey struct {
	ProgramID string
	EdgeID    int64
}

// cachedProgram pins the predicate source alongside the compiled
// program so a duplicate-key Eval whose predicate text was rewritten
// without an explicit InvalidateProgram still recompiles. The cache
// thus tolerates re-plan paths that swap the predicate string while
// retaining the same edge row ID.
type cachedProgram struct {
	predicate string
	program   cel.Program
}

// NewEdgeEvaluator returns an empty evaluator ready for concurrent use.
func NewEdgeEvaluator() *EdgeEvaluator {
	return &EdgeEvaluator{}
}

// Eval runs edge.PredicateCEL against journal.OutputJSON.
//
// Empty predicate ⇒ (true, "unconditional", nil) — the spec treats
// edges without a predicate as always-fire edges, equivalent to v1
// depends_on_features semantics lifted to v2 wire form.
//
// Compile failures wrap ErrPredicateCompile (caller treats as a config
// bug — should have been caught at ValidateV2). Eval failures (non-bool
// result, journal JSON parse failure, runtime resolution failure) wrap
// ErrPredicateEval — distinct sentinel so the scheduler routes
// operator alerts away from compile-shaped triage.
func (e *EdgeEvaluator) Eval(ctx context.Context, edge state.EdgeRow, schema *OutputsSchema, journal state.OutputJournalEntry) (fired bool, reason string, err error) {
	if edge.PredicateCEL == "" {
		return true, "unconditional", nil
	}
	prg, err := e.programFor(edge)
	if err != nil {
		return false, "", err
	}
	var out any
	if err := json.Unmarshal([]byte(journal.OutputJSON), &out); err != nil {
		return false, "", fmt.Errorf("%w: edge=%d parse journal: %w",
			orchestrator.ErrPredicateEval, edge.ID, err)
	}
	val, _, err := prg.ContextEval(ctx, map[string]any{"out": out})
	if err != nil {
		return false, "", fmt.Errorf("%w: edge=%d: %w",
			orchestrator.ErrPredicateEval, edge.ID, err)
	}
	b, ok := val.(types.Bool)
	if !ok {
		return false, "", fmt.Errorf("%w: edge=%d predicate returned %s, want bool",
			orchestrator.ErrPredicateEval, edge.ID, typeName(val))
	}
	if bool(b) {
		return true, "predicate=true", nil
	}
	return false, "predicate=false", nil
}

// InvalidateProgram drops every cached compile for one programID.
// Called by BriefLoader.Sync when a brief is removed or its edge set is
// rewritten. Matches the planner_v2 contract: a re-plan that changes
// predicate text must not silently keep stale compiled programs alive.
func (e *EdgeEvaluator) InvalidateProgram(programID string) {
	e.cache.Range(func(k, _ any) bool {
		if key, ok := k.(edgeCacheKey); ok && key.ProgramID == programID {
			e.cache.Delete(key)
		}
		return true
	})
}

// programFor returns the cached cel.Program for one edge, compiling
// on first sight or when the cached entry's predicate text drifted
// from the row's current text. Concurrent first-compile callers race
// benignly: the second store wins, the first compile is discarded —
// cheap because compile is pure.
func (e *EdgeEvaluator) programFor(edge state.EdgeRow) (cel.Program, error) {
	key := edgeCacheKey{ProgramID: edge.ProgramID, EdgeID: edge.ID}
	if v, ok := e.cache.Load(key); ok {
		if entry, ok := v.(*cachedProgram); ok && entry.predicate == edge.PredicateCEL {
			return entry.program, nil
		}
	}
	env, err := buildPredicateEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: edge=%d build env: %w",
			orchestrator.ErrPredicateCompile, edge.ID, err)
	}
	ast, iss := env.Compile(edge.PredicateCEL)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("%w: edge=%d: %w",
			orchestrator.ErrPredicateCompile, edge.ID, iss.Err())
	}
	prg, err := env.Program(ast, cel.CostLimit(10_000))
	if err != nil {
		return nil, fmt.Errorf("%w: edge=%d program: %w",
			orchestrator.ErrPredicateCompile, edge.ID, err)
	}
	e.compiles.Add(1)
	e.cache.Store(key, &cachedProgram{predicate: edge.PredicateCEL, program: prg})
	return prg, nil
}

// compileCount reports how many cel.Program builds occurred since the
// evaluator was constructed. Test-only seam exercised by
// TestEdgeEvaluator_CacheHit / TestEdgeEvaluator_InvalidateProgram to
// pin the cache-miss vs cache-hit boundary without instrumenting the
// runtime path.
func (e *EdgeEvaluator) compileCount() int64 {
	return e.compiles.Load()
}

func typeName(v ref.Val) string {
	if v == nil || v.Type() == nil {
		return "<nil>"
	}
	return v.Type().TypeName()
}

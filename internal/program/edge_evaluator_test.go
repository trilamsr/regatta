package program

import (
	"context"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func sevHighSchema() *OutputsSchema {
	return &OutputsSchema{
		Type: "object",
		Properties: map[string]*OutputsSchema{
			"severity": {Type: "string", Enum: []any{"high", "low"}},
		},
	}
}

func journal(s string) state.OutputJournalEntry {
	return state.OutputJournalEntry{OutputJSON: s, ContentSHA: "sha:" + s}
}

func TestEdgeEvaluator_UnconditionalReturnsTrue(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{ID: 1, ProgramID: "m-1", FromID: "F-A", ToID: "F-B"}
	fired, reason, err := ev.Eval(context.Background(), edge, nil, journal(`{}`))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !fired {
		t.Fatalf("unconditional edge must fire")
	}
	if reason != "unconditional" {
		t.Fatalf("reason=%q want unconditional", reason)
	}
}

func TestEdgeEvaluator_PredicateTrue(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{
		ID: 2, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity == "high"`,
	}
	fired, _, err := ev.Eval(context.Background(), edge, sevHighSchema(),
		journal(`{"severity":"high"}`))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !fired {
		t.Fatalf("expected fired=true for severity=high")
	}
}

func TestEdgeEvaluator_PredicateFalse(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{
		ID: 3, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity == "high"`,
	}
	fired, _, err := ev.Eval(context.Background(), edge, sevHighSchema(),
		journal(`{"severity":"low"}`))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if fired {
		t.Fatalf("expected fired=false for severity=low")
	}
}

func TestEdgeEvaluator_CacheHit(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{
		ID: 7, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity == "high"`,
	}
	for i := 0; i < 3; i++ {
		if _, _, err := ev.Eval(context.Background(), edge, sevHighSchema(),
			journal(`{"severity":"high"}`)); err != nil {
			t.Fatalf("eval #%d: %v", i, err)
		}
	}
	if got := ev.compileCount(); got != 1 {
		t.Fatalf("compileCount=%d want 1 (cache miss recompiled)", got)
	}
}

func TestEdgeEvaluator_InvalidateProgram(t *testing.T) {
	ev := NewEdgeEvaluator()
	edgeA := state.EdgeRow{ID: 10, ProgramID: "m-A", FromID: "F1", ToID: "F2",
		PredicateCEL: `out.severity == "high"`}
	edgeB := state.EdgeRow{ID: 11, ProgramID: "m-B", FromID: "F1", ToID: "F2",
		PredicateCEL: `out.severity == "high"`}
	if _, _, err := ev.Eval(context.Background(), edgeA, sevHighSchema(),
		journal(`{"severity":"high"}`)); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, _, err := ev.Eval(context.Background(), edgeB, sevHighSchema(),
		journal(`{"severity":"high"}`)); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	if got := ev.compileCount(); got != 2 {
		t.Fatalf("setup compileCount=%d want 2", got)
	}

	ev.InvalidateProgram("m-A")

	if _, _, err := ev.Eval(context.Background(), edgeA, sevHighSchema(),
		journal(`{"severity":"high"}`)); err != nil {
		t.Fatalf("re-eval A: %v", err)
	}
	if _, _, err := ev.Eval(context.Background(), edgeB, sevHighSchema(),
		journal(`{"severity":"high"}`)); err != nil {
		t.Fatalf("re-eval B: %v", err)
	}
	if got := ev.compileCount(); got != 3 {
		t.Fatalf("post-invalidate compileCount=%d want 3 (A recompiled, B cached)", got)
	}
}

func TestEdgeEvaluator_JournalMalformed(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{
		ID: 20, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity == "high"`,
	}
	_, _, err := ev.Eval(context.Background(), edge, sevHighSchema(),
		journal(`{not json`))
	if err == nil {
		t.Fatalf("malformed journal must error")
	}
	if !errors.Is(err, orchestrator.ErrPredicateEval) {
		t.Fatalf("got %v want ErrPredicateEval wrap", err)
	}
}

func TestEdgeEvaluator_NestedField(t *testing.T) {
	ev := NewEdgeEvaluator()
	schema := &OutputsSchema{
		Type: "object",
		Properties: map[string]*OutputsSchema{
			"a": {
				Type: "object",
				Properties: map[string]*OutputsSchema{
					"b": {Type: "int"},
				},
			},
		},
	}
	edge := state.EdgeRow{
		ID: 30, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.a.b == 1`,
	}
	fired, _, err := ev.Eval(context.Background(), edge, schema,
		journal(`{"a":{"b":1}}`))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !fired {
		t.Fatalf("nested field equality should fire")
	}
}

func TestEdgeEvaluator_NonBoolPredicate(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{
		ID: 40, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity`,
	}
	_, _, err := ev.Eval(context.Background(), edge, sevHighSchema(),
		journal(`{"severity":"high"}`))
	if err == nil {
		t.Fatalf("non-bool predicate must error")
	}
	if !errors.Is(err, orchestrator.ErrPredicateEval) {
		t.Fatalf("got %v want ErrPredicateEval", err)
	}
}

func TestEdgeEvaluator_CompileError(t *testing.T) {
	ev := NewEdgeEvaluator()
	edge := state.EdgeRow{
		ID: 50, ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity ==`,
	}
	_, _, err := ev.Eval(context.Background(), edge, sevHighSchema(),
		journal(`{"severity":"high"}`))
	if err == nil {
		t.Fatalf("syntax error must surface")
	}
	if !errors.Is(err, orchestrator.ErrPredicateCompile) {
		t.Fatalf("got %v want ErrPredicateCompile wrap", err)
	}
}

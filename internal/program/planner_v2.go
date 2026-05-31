// ValidateV2 enforces every v1 invariant plus the conditional-edge
// rules introduced by the v2 wire format (spec §3.3):
//
//  1. edge endpoints reference known feature IDs (ErrEdgeUnknownTarget)
//  2. ≥1 predicated outgoing edge ⇒ DefaultNext non-nil (ErrEdgeMissingDefault)
//  3. every predicate compiles under CEL with `out` typed to the
//     upstream's OutputsSchema (ErrPredicateCompile)
//  4. predicate references only out.<field> paths declared in the
//     upstream's OutputsSchema (ErrPredicateUnknownField)
//  5. predicate literal comparisons type-match the declared property
//     type (ErrPredicateTypeMismatch)
//  6. union of all edges is acyclic (delegates to v1 checkDAG)
//
// The cel-go env uses MapType(string,dyn) — strict per-field typing is
// enforced by post-Compile AST walk so unknown-field and type-mismatch
// surface as distinct sentinels instead of being collapsed under one
// generic compile error.

package program

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/types/ref"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// ValidateV2 runs structural + CEL-typed predicate compile checks
// over a v2 brief. Returns wrapped sentinels so callers can switch on
// errors.Is.
//
// v2 skips the v1 coverage gate: in v2 a feature with empty Fulfills
// is legal because it may exist solely as a conditional branch
// triggered by an upstream predicate. The schema, identifier-shape,
// and DAG gates are still enforced.
func (p *ProgramBriefV2) ValidateV2() error {
	if p.SchemaVersion != 2 {
		return fmt.Errorf("%w: schema_version must be 2, got %d", ErrPlanSchemaInvalid, p.SchemaVersion)
	}
	if !programIDRe.MatchString(p.ProgramID) {
		return fmt.Errorf("%w: bad program_id %q", ErrPlanSchemaInvalid, p.ProgramID)
	}
	if p.ParentWorkItemID == "" {
		return fmt.Errorf("%w: parent_work_item_id required", ErrPlanSchemaInvalid)
	}
	if len(p.ParentCriteria) == 0 {
		return fmt.Errorf("%w: parent_criteria must not be empty", ErrPlanSchemaInvalid)
	}
	if len(p.FeaturesV2) == 0 {
		return fmt.Errorf("%w: features must not be empty", ErrPlanSchemaInvalid)
	}
	seenFeat := map[string]bool{}
	for _, f := range p.FeaturesV2 {
		if !featureIDRe.MatchString(f.ID) {
			return fmt.Errorf("%w: bad feature id %q", ErrPlanSchemaInvalid, f.ID)
		}
		if seenFeat[f.ID] {
			return fmt.Errorf("%w: duplicate feature id %q", ErrPlanSchemaInvalid, f.ID)
		}
		seenFeat[f.ID] = true
		if f.Title == "" {
			return fmt.Errorf("%w: feature %s missing title", ErrPlanSchemaInvalid, f.ID)
		}
		if !allowedComplexities[f.EstimatedComplexity] {
			return fmt.Errorf("%w: feature %s estimated_complexity %q invalid", ErrPlanSchemaInvalid, f.ID, f.EstimatedComplexity)
		}
	}

	known := make(map[string]*PlannedFeatureV2, len(p.FeaturesV2))
	for i := range p.FeaturesV2 {
		known[p.FeaturesV2[i].ID] = &p.FeaturesV2[i]
	}

	for i := range p.FeaturesV2 {
		f := &p.FeaturesV2[i]
		hasPredicated := false
		for _, e := range f.Edges {
			if e.From != f.ID {
				return fmt.Errorf("%w: edge.From=%s declared on feature %s",
					orchestrator.ErrEdgeUnknownTarget, e.From, f.ID)
			}
			if _, ok := known[e.To]; !ok {
				return fmt.Errorf("%w: %s -> %s", orchestrator.ErrEdgeUnknownTarget, e.From, e.To)
			}
			if e.OnSkip != "" && e.OnSkip != SkipCascade && e.OnSkip != SkipIgnore && e.OnSkip != SkipDefault {
				return fmt.Errorf("%w: edge %s->%s on_skip=%q invalid",
					orchestrator.ErrEdgeUnknownTarget, e.From, e.To, e.OnSkip)
			}
			if e.Predicate == "" {
				continue
			}
			hasPredicated = true
			if err := compilePredicate(e.Predicate, f.OutputsSchema); err != nil {
				return err
			}
		}
		if hasPredicated && f.DefaultNext == nil {
			return fmt.Errorf("%w: feature %s has predicated edges but no default_next",
				orchestrator.ErrEdgeMissingDefault, f.ID)
		}
		if f.DefaultNext != nil {
			if _, ok := known[*f.DefaultNext]; !ok {
				return fmt.Errorf("%w: feature %s default_next=%s",
					orchestrator.ErrEdgeUnknownTarget, f.ID, *f.DefaultNext)
			}
		}
	}

	// Cycle check: lower edges into a v1-shaped DependsOnFeatures view
	// on a scratch ProgramBrief so we can reuse checkDAG.
	scratch := p.ProgramBrief
	scratch.Features = make([]PlannedFeature, len(p.FeaturesV2))
	for i, f := range p.FeaturesV2 {
		copyf := f.PlannedFeature
		for _, e := range f.Edges {
			copyf.DependsOnFeatures = append(copyf.DependsOnFeatures, e.To)
		}
		scratch.Features[i] = copyf
	}
	return scratch.checkDAG()
}

// compilePredicate builds a CEL env from schema, compiles the
// predicate, then walks the AST to surface field-existence and
// literal-type errors as distinct sentinels.
func compilePredicate(predicate string, schema *OutputsSchema) error {
	env, err := cel.NewEnv(cel.Variable("out", cel.MapType(cel.StringType, cel.DynType)))
	if err != nil {
		return fmt.Errorf("%w: build env: %w", orchestrator.ErrPredicateCompile, err)
	}
	ast, iss := env.Compile(predicate)
	if iss != nil && iss.Err() != nil {
		msg := iss.Err().Error()
		switch {
		case strings.Contains(msg, "undeclared reference"),
			strings.Contains(msg, "no such field"),
			strings.Contains(msg, "undefined field"):
			return fmt.Errorf("%w: %s", orchestrator.ErrPredicateUnknownField, msg)
		case strings.Contains(msg, "no matching overload"):
			return fmt.Errorf("%w: %s", orchestrator.ErrPredicateTypeMismatch, msg)
		default:
			return fmt.Errorf("%w: %s", orchestrator.ErrPredicateCompile, msg)
		}
	}
	if ast.OutputType() != cel.BoolType {
		return fmt.Errorf("%w: predicate must return bool, got %s",
			orchestrator.ErrPredicateTypeMismatch, ast.OutputType().String())
	}

	// Strict per-field checks against schema. MapType(string,dyn)
	// accepts any field name + any literal type at compile time, so
	// we walk the AST post-compile to enforce the declared
	// OutputsSchema contract.
	return checkPredicateAgainstSchema(ast, schema)
}

// checkPredicateAgainstSchema walks the parsed AST and rejects:
//   - any out.<field> selector whose <field> is absent from
//     schema.Properties (ErrPredicateUnknownField)
//   - any equality/inequality comparison between out.<field> and a
//     literal whose runtime type disagrees with the declared property
//     type (ErrPredicateTypeMismatch)
func checkPredicateAgainstSchema(ast *cel.Ast, schema *OutputsSchema) error {
	if schema == nil || schema.Type != "object" || len(schema.Properties) == 0 {
		return nil
	}
	native := ast.NativeRep()
	nav := celast.NavigateAST(native)

	for _, sel := range celast.MatchDescendants(nav, celast.KindMatcher(celast.SelectKind)) {
		field, ok := outFieldName(sel)
		if !ok {
			continue
		}
		if _, declared := schema.Properties[field]; !declared {
			return fmt.Errorf("%w: predicate references out.%s not declared in outputs_schema",
				orchestrator.ErrPredicateUnknownField, field)
		}
	}

	for _, call := range celast.MatchDescendants(nav, celast.KindMatcher(celast.CallKind)) {
		c := call.AsCall()
		fn := c.FunctionName()
		if fn != "_==_" && fn != "_!=_" && fn != "_<_" && fn != "_<=_" && fn != "_>_" && fn != "_>=_" {
			continue
		}
		args := c.Args()
		if len(args) != 2 {
			continue
		}
		var (
			field   string
			lit     ref.Val
			matched bool
		)
		switch {
		case args[0].Kind() == celast.SelectKind && args[1].Kind() == celast.LiteralKind:
			if f, ok := outFieldName(args[0]); ok {
				field, lit, matched = f, args[1].AsLiteral(), true
			}
		case args[1].Kind() == celast.SelectKind && args[0].Kind() == celast.LiteralKind:
			if f, ok := outFieldName(args[1]); ok {
				field, lit, matched = f, args[0].AsLiteral(), true
			}
		}
		if !matched {
			continue
		}
		prop, ok := schema.Properties[field]
		if !ok {
			continue
		}
		want := celTypeNameFor(prop.Type)
		got := lit.Type().TypeName()
		if want != "" && got != "" && want != got {
			return fmt.Errorf("%w: out.%s declared %s, predicate compares against %s",
				orchestrator.ErrPredicateTypeMismatch, field, prop.Type, got)
		}
	}
	return nil
}

func outFieldName(e celast.Expr) (string, bool) {
	if e.Kind() != celast.SelectKind {
		return "", false
	}
	s := e.AsSelect()
	op := s.Operand()
	if op.Kind() != celast.IdentKind || op.AsIdent() != "out" {
		return "", false
	}
	return s.FieldName(), true
}

// celTypeNameFor maps OutputsSchema property type strings onto cel-go
// runtime type names (the string returned by ref.Val.Type().TypeName()).
func celTypeNameFor(t string) string {
	switch t {
	case "string":
		return "string"
	case "int", "integer":
		return "int"
	case "double", "number":
		return "double"
	case "bool", "boolean":
		return "bool"
	case "list", "array":
		return "list"
	case "map", "object":
		return "map"
	default:
		return ""
	}
}

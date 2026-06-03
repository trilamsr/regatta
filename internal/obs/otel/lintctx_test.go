package otel_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

// TestGoroutineCtxPropagation_LintCheck — W6 spec A+2 goroutine-ctx-propagation lint over 8 T5 files.
func TestGoroutineCtxPropagation_LintCheck(t *testing.T) {
	repoRoot := reporoot.Must(t)
	t5TouchedFiles := []string{
		"internal/orchestrator/orchestrator.go",
		"internal/orchestrator/scheduler/scheduler.go",
		"internal/orchestrator/spawner/spawner.go",
		"internal/orchestrator/reaper/reaper.go",
		"internal/orchestrator/adaptersync/adaptersync.go",
		"internal/gates/approval/gate.go",
		"internal/program/brief_loader.go",
		"internal/orchestrator/adapter/markdown.go",
	}
	fset := token.NewFileSet()
	var failures []string
	for _, rel := range t5TouchedFiles {
		path := filepath.Join(repoRoot, rel)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			if !funcTakesCtx(fn.Type) {
				return true
			}
			// Walk the function body for `go func(...) {}(...)` calls.
			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				goStmt, ok := inner.(*ast.GoStmt)
				if !ok {
					return true
				}
				// goStmt.Call.Fun is the FuncLit (literal) or an
				// identifier; only a FuncLit is the "go func()" form.
				lit, ok := goStmt.Call.Fun.(*ast.FuncLit)
				if !ok {
					return true
				}
				// If the closure body references ctx-bearing surface
				// (variable named "ctx" OR a call to tracer.Start /
				// span.End / slog primitives that span propagation cares
				// about), require the goroutine to take it as an arg.
				if !closureUsesCtxOrTracer(lit.Body) {
					return true
				}
				if !funcLitTakesCtx(lit.Type, goStmt.Call.Args) {
					pos := fset.Position(goStmt.Pos())
					failures = append(failures, pos.String()+": goroutine in ctx-bearing func closes over ctx without explicit arg passing")
				}
				return true
			})
			return true
		})
	}
	if len(failures) > 0 {
		t.Fatalf("goroutine ctx propagation lint failures (%d):\n  %s",
			len(failures), strings.Join(failures, "\n  "))
	}
}

// funcTakesCtx reports whether fn's parameter list includes a
// context.Context-typed parameter.
func funcTakesCtx(fn *ast.FuncType) bool {
	if fn == nil || fn.Params == nil {
		return false
	}
	for _, field := range fn.Params.List {
		if isCtxType(field.Type) {
			return true
		}
	}
	return false
}

// funcLitTakesCtx reports whether the goroutine's func literal both
// declares a context.Context parameter AND receives a concrete value
// at the call site (positional arg matching the ctx slot).
func funcLitTakesCtx(lit *ast.FuncType, args []ast.Expr) bool {
	if lit == nil || lit.Params == nil {
		return false
	}
	for i, field := range lit.Params.List {
		if !isCtxType(field.Type) {
			continue
		}
		// The positional arg at index i must be supplied. A bare
		// closure-over-ctx never has args.
		if i < len(args) {
			return true
		}
	}
	return false
}

// isCtxType reports whether expr is the context.Context type.
func isCtxType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return x.Name == "context" && sel.Sel.Name == "Context"
}

// closureUsesCtxOrTracer scans the body for identifiers / calls that
// indicate span-context awareness: `ctx`, `tracer.Start`, `span.End`.
func closureUsesCtxOrTracer(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch v := n.(type) {
		case *ast.Ident:
			if v.Name == "ctx" {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "Start" || v.Sel.Name == "End" || v.Sel.Name == "SpanFromContext" {
				found = true
			}
		}
		return !found
	})
	return found
}


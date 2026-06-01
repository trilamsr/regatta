package otel_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoroutineCtxPropagation_LintCheck — W6 spec A+2 stretch rubric
// + §9 R3 risk mitigation. AST-walks the 8 T5-touched files and asserts
// no `go func() {}()` closure references a span-bearing ctx without
// explicit passing. Specifically: when the enclosing function takes
// a ctx parameter and a goroutine launched inside it uses `ctx` or
// `tracer.Start`, the goroutine MUST receive ctx as a function arg
// rather than closing over the outer ctx (which may end before the
// goroutine runs).
//
// The check is intentionally narrow: only the 8 files modified by W6
// T5 are scanned. Future components opt in by adding their path to
// the t5TouchedFiles list. A repo-wide variant lives behind a
// followup tracking issue if/when other packages take spans.
func TestGoroutineCtxPropagation_LintCheck(t *testing.T) {
	repoRoot := mustRepoRoot(t)
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

// mustRepoRoot returns the absolute path of the module root by
// walking upward from the test's working directory until a go.mod
// is found. Keeps the lint test pinnable from any package depth.
func mustRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs cwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("repo root (go.mod) not found upward from cwd")
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

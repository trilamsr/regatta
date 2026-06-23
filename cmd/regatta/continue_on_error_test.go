package main

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestParse_RestoresViaDefer asserts cmd/regatta CLI helpers use flag.ContinueOnError so parse failure returns through defers not os.Exit (R-MEGA-3 LIVE-15).
func TestParse_RestoresViaDefer(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "flag" {
				return true
			}
			if sel.Sel.Name != "ExitOnError" {
				return true
			}
			t.Errorf("%s:%d: flag.ExitOnError leaks os.Exit past deferred cleanup; switch to flag.ContinueOnError + return-on-err",
				path, fset.Position(sel.Pos()).Line)
			return true
		})
	}
	// Compile-time sanity: ContinueOnError is the constant we expect.
	if flag.ContinueOnError == flag.ExitOnError {
		t.Fatal("flag package constants identical; test would be vacuous")
	}
}

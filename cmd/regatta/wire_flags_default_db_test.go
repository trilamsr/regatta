package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestWireFlags_NoBareRegattaDBLiteral asserts wire_flags.go references stateDBDefaultLiteral, not a duplicate bare "regatta.db" string (R-MEGA-3 LIVE-17).
func TestWireFlags_NoBareRegattaDBLiteral(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "wire_flags.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if strings.Trim(lit.Value, `"`) == "regatta.db" {
			t.Errorf("wire_flags.go:%d: bare \"regatta.db\" literal — use stateDBDefaultLiteral const", fset.Position(lit.Pos()).Line)
		}
		return true
	})
}

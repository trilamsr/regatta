// Package lint_test holds the repo-wide invariant lints that catch
// drift across package boundaries the per-package tests cannot see.
// This file enforces the Wave B2 invariant: every non-test production
// site reaches metric.Meter through internal/obs.Meter(scope) rather
// than otel.Meter("string-literal"), so scope-name churn touches one
// const block instead of N call sites — a dashboard query keyed on
// "scheduler" survives any rename because the const value is the
// authoritative copy of that string.
package obs_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

// TestMeterScope_NoLiteralOutsideObs asserts every prod call to otel.Meter passes a string-literal argument only from within internal/obs/ (Wave B2).
func TestMeterScope_NoLiteralOutsideObs(t *testing.T) {
	repoRoot := reporoot.Must(t)
	fset := token.NewFileSet()
	var violations []string

	walkRoot := filepath.Join(repoRoot, "internal")
	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == "vendor" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/obs/") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Meter" {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "otel" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			pos := fset.Position(call.Pos())
			violations = append(violations,
				pos.String()+": otel.Meter("+lit.Value+") — use obs.Meter(obs.MeterScopeX) instead")
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("otel.Meter literal violations (%d) — every prod call MUST route through obs.Meter(obs.MeterScopeX):\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// TestMeterScope_ConstsMatchHistoricalValues asserts every MeterScope const string equals its historical otel.Meter scope name (dashboard query keys).
func TestMeterScope_ConstsMatchHistoricalValues(t *testing.T) {
	want := map[string]string{
		"MeterScopeOrchestrator":         "orchestrator",
		"MeterScopeScheduler":            "scheduler",
		"MeterScopeSchedulerFallback":    "scheduler-fallback",
		"MeterScopeReaper":               "reaper",
		"MeterScopeSpawner":              "spawner",
		"MeterScopeAdaptersync":          "adaptersync",
		"MeterScopeAlerthook":         "alerthook",
		"MeterScopeAlerthookFallback": "alerthook-fallback",
		"MeterScopeObsTriggers":          "obs/triggers",
		"MeterScopeObsAdversarial":       "obs/adversarial",
		"MeterScopeCostCap":              "internal/cost/cap",
		"MeterScopeProgram":              "program",
		"MeterScopeSubstrate":            "orchestrator/state/substrate",
	}
	repoRoot := reporoot.Must(t)
	fset := token.NewFileSet()
	path := filepath.Join(repoRoot, "internal", "obs", "meterscope.go")
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse meterscope.go: %v", err)
	}
	got := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			got[vs.Names[0].Name] = strings.Trim(lit.Value, `"`)
		}
	}
	for name, wantVal := range want {
		if gotVal, ok := got[name]; !ok {
			t.Errorf("MeterScope const %s missing — dashboard query for %q breaks if absent", name, wantVal)
		} else if gotVal != wantVal {
			t.Errorf("MeterScope const %s = %q, want %q — renaming this const silently drifts the dashboard scope name", name, gotVal, wantVal)
		}
	}
}

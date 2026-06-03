// Package lint_test holds repo-wide invariant lints that catch
// drift across package boundaries the per-package tests cannot
// see — here, the "every Tracer-bearing DI struct also carries a
// Meter seam" invariant from W6 (#509). One repo-walk per CI run
// keeps the gate cheap and the violation list visible.
package lint_test

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

// knownGap tracks structs intentionally exempt from the Tracer+Meter
// pair invariant pending their per-struct retrofit. Each entry MUST
// cite the tracking issue (#509 is the umbrella) and is removed when
// that struct gains its Meter sibling. New Tracer-bearing structs
// MUST land with Meter — the allowlist is closed to drift, not
// open-ended; CI fails on any violation outside this set.
var knownGap = map[string]string{
	"internal/authz/opa.go:Config":                            "#509",
	"internal/authz/policies/reload/reloader.go:Reloader":     "#509",
	"internal/cost/gate/gate.go:Config":                       "#509",
	"internal/cost/reconcile/tick.go:Config":                  "#509",
	"internal/gates/approval/gate.go:Gate":                    "#509",
	"internal/gates/l4/reload.go:Reloader":                    "#509",
	"internal/orchestrator/adapter/markdown.go:MarkdownCatalogConfig": "#509",
	"internal/orchestrator/prwatch/prwatch.go:Config":         "#509",
	"internal/orchestrator/spawner/claude.go:ClaudeSpawnerConfig": "#509",
}

// TestTracerMeterPair_EveryTracerHasMeter asserts every struct carrying a `Tracer trace.Tracer` field also carries `Meter metric.Meter` (#509).
func TestTracerMeterPair_EveryTracerHasMeter(t *testing.T) {
	repoRoot := reporoot.Must(t)
	fset := token.NewFileSet()
	var violations []string
	seen := map[string]struct{}{}

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
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			hasTracer, hasMeter := false, false
			for _, field := range st.Fields.List {
				if !isFieldType(field, "trace", "Tracer") && !isFieldType(field, "metric", "Meter") {
					continue
				}
				for _, name := range field.Names {
					switch name.Name {
					case "Tracer":
						if isFieldType(field, "trace", "Tracer") {
							hasTracer = true
						}
					case "Meter":
						if isFieldType(field, "metric", "Meter") {
							hasMeter = true
						}
					}
				}
			}
			if hasTracer && !hasMeter {
				pos := fset.Position(ts.Pos())
				rel, err := filepath.Rel(repoRoot, pos.Filename)
				if err != nil {
					rel = pos.Filename
				}
				rel = filepath.ToSlash(rel)
				key := rel + ":" + ts.Name.Name
				seen[key] = struct{}{}
				if _, ok := knownGap[key]; ok {
					return true
				}
				violations = append(violations,
					pos.String()+": struct "+ts.Name.Name+" carries Tracer trace.Tracer but no Meter metric.Meter sibling")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Tracer-without-Meter violations (%d) — every Tracer-bearing struct MUST carry a Meter sibling (#509):\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
	for key, ref := range knownGap {
		if _, hit := seen[key]; !hit {
			t.Errorf("knownGap stale: %s (%s) no longer violates — remove from allowlist", key, ref)
		}
	}
}

// isFieldType reports whether f.Type is the selector pkg.name.
func isFieldType(f *ast.Field, pkg, name string) bool {
	sel, ok := f.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkgIdent.Name == pkg && sel.Sel.Name == name
}

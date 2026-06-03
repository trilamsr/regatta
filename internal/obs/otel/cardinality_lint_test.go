package otel_test

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

// TestMetricCardinality_PRNumberLabelBanned walks the repo for high-cardinality label literals on metric instruments.
func TestMetricCardinality_PRNumberLabelBanned(t *testing.T) {
	repoRoot := reporoot.Must(t)
	banned := map[string]struct{}{
		"pr_number":    {},
		"run_id":       {},
		"work_item_id": {},
	}
	// Metric instrument constructor selectors emitted from meter.* —
	// the lint walks ONLY call sites whose receiver is a metric.Meter
	// and selector matches one of these names.
	instrumentSel := map[string]struct{}{
		"Int64Counter":         {},
		"Float64Counter":       {},
		"Int64UpDownCounter":   {},
		"Float64UpDownCounter": {},
		"Int64Histogram":       {},
		"Float64Histogram":     {},
		"Int64Gauge":           {},
		"Float64Gauge":         {},
	}

	fset := token.NewFileSet()
	var failures []string

	walkRoot := filepath.Join(repoRoot, "internal")
	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip testdata + vendored trees.
			name := d.Name()
			if name == "testdata" || name == "vendor" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Lint scans non-test source — instruments live in production code.
		if strings.HasSuffix(path, "_test.go") {
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
			if !ok {
				return true
			}
			if _, hit := instrumentSel[sel.Sel.Name]; !hit {
				return true
			}
			// First arg is the instrument name; later args are options
			// containing the WithAttributes / metric.WithAttributeSet
			// payloads we treat as cardinality fences. The lint screens
			// every string literal in the call for banned label names.
			for _, arg := range call.Args {
				ast.Inspect(arg, func(inner ast.Node) bool {
					lit, ok := inner.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					raw := strings.Trim(lit.Value, "\"`")
					if _, bad := banned[raw]; bad {
						pos := fset.Position(lit.Pos())
						failures = append(failures,
							pos.String()+": metric instrument carries banned high-cardinality label "+raw)
					}
					return true
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	if len(failures) > 0 {
		t.Fatalf("metric cardinality lint failures (%d):\n  %s",
			len(failures), strings.Join(failures, "\n  "))
	}
}

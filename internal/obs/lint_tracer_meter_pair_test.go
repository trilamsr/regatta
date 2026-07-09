// Package lint_test holds repo-wide invariant lints that catch
// drift across package boundaries the per-package tests cannot
// see — here, the "every Tracer-bearing DI struct also carries a
// Meter seam" invariant from W6 (#509). One repo-walk per CI run
// keeps the gate cheap and the violation list visible.
package obs_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

var knownGap = map[string]string{
	"internal/authz/opa.go:Config":                                    "#509",
	"internal/authz/policies/reload/reloader.go:Reloader":             "#509",
	"internal/cost/gate/gate.go:Config":                               "#509",
	"internal/cost/reconcile/tick.go:Config":                          "#509",
	"internal/gates/approval/gate.go:Gate":                            "#509",
	"internal/orchestrator/adapter/markdown.go:MarkdownCatalogConfig": "#509",
	"internal/orchestrator/spawner/claude.go:ClaudeSpawnerConfig":     "#509",
}

// TestTracerMeterPair_EveryTracerHasMeter asserts every struct carrying a `Tracer trace.Tracer` field also carries `Meter metric.Meter` (#509).
func TestTracerMeterPair_EveryTracerHasMeter(t *testing.T) {
	repoRoot := reporoot.Must(t)
	walkRoot := filepath.Join(repoRoot, "internal")
	violations, seen, stale, err := lintTracerMeterPair(walkRoot, repoRoot, knownGap)
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Tracer-without-Meter violations (%d) — every Tracer-bearing struct MUST carry a Meter sibling (#509):\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
	for _, key := range stale {
		t.Errorf("knownGap stale: %s (%s) no longer violates — remove from allowlist", key, knownGap[key])
	}
	_ = seen
}

// TestTracerMeterPair_DetectsEmbeddedTracer asserts an embedded type that carries Tracer-without-Meter is flagged (#706 R1).
func TestTracerMeterPair_DetectsEmbeddedTracer(t *testing.T) {
	root := filepath.Join("testdata", "r1_embedded")
	violations, _, _, err := lintTracerMeterPair(root, root, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !containsViolation(violations, "EmbedsLeakyInner") {
		t.Fatalf("expected EmbedsLeakyInner flagged via embedded LeakyInner, got: %v", violations)
	}
	if containsViolation(violations, "EmbedsInnerOK") {
		t.Fatalf("EmbedsInnerOK embeds Inner which has both — must not flag, got: %v", violations)
	}
}

// TestTracerMeterPair_DetectsAliasTracer asserts a field typed as a local alias of trace.Tracer is flagged (#706 R2).
func TestTracerMeterPair_DetectsAliasTracer(t *testing.T) {
	root := filepath.Join("testdata", "r2_alias")
	violations, _, _, err := lintTracerMeterPair(root, root, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !containsViolation(violations, "WithAliasLeaky") {
		t.Fatalf("expected WithAliasLeaky flagged via MyTracer alias, got: %v", violations)
	}
	if containsViolation(violations, "WithAliasOK") {
		t.Fatalf("WithAliasOK has both aliases — must not flag, got: %v", violations)
	}
}

// TestTracerMeterPair_StaleAllowlistEntryFails asserts an allowlist key pointing to a non-existent struct is reported stale (#706 R3).
func TestTracerMeterPair_StaleAllowlistEntryFails(t *testing.T) {
	root := filepath.Join("testdata", "r3_stale")
	allow := map[string]string{
		"testdata/r3_stale/pkgc/empty.go:DoesNotExist": "#706",
	}
	_, _, stale, err := lintTracerMeterPair(root, root, allow)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(stale) == 0 {
		t.Fatalf("expected stale entry for DoesNotExist, got none")
	}
	if !strings.Contains(stale[0], "DoesNotExist") {
		t.Fatalf("expected stale to cite DoesNotExist, got %q", stale[0])
	}
}

// TestTracerMeterPair_StaleAllowlistEntryFiresOnRetrofit asserts an allowlist key whose struct no longer leaks is reported stale (#706 R3).
func TestTracerMeterPair_StaleAllowlistEntryFiresOnRetrofit(t *testing.T) {
	root := filepath.Join("testdata", "r1_embedded")
	allow := map[string]string{
		"testdata/r1_embedded/pkga/embedded.go:Inner": "#706",
	}
	_, _, stale, err := lintTracerMeterPair(root, root, allow)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	found := false
	for _, s := range stale {
		if strings.Contains(s, "Inner") && !strings.Contains(s, "LeakyInner") && !strings.Contains(s, "EmbedsInner") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stale entry for Inner (has both fields), got %v", stale)
	}
}

func containsViolation(vs []string, structName string) bool {
	for _, v := range vs {
		if strings.Contains(v, "struct "+structName+" ") {
			return true
		}
	}
	return false
}

func lintTracerMeterPair(walkRoot, relRoot string, allow map[string]string) (violations []string, seen map[string]struct{}, stale []string, err error) {
	seen = map[string]struct{}{}
	fset := token.NewFileSet()

	pkgs, walkErr := loadStructIndex(walkRoot, fset)
	if walkErr != nil {
		return nil, nil, nil, walkErr
	}

	for _, pkg := range pkgs {
		for _, st := range pkg.structs {
			hasTracer, hasMeter := pkgHasPair(pkg, st.spec.Name.Name, map[string]bool{})
			if !hasTracer || hasMeter {
				continue
			}
			pos := fset.Position(st.spec.Pos())
			rel, rerr := filepath.Rel(relRoot, pos.Filename)
			if rerr != nil {
				rel = pos.Filename
			}
			rel = filepath.ToSlash(rel)
			key := rel + ":" + st.spec.Name.Name
			seen[key] = struct{}{}
			if _, ok := allow[key]; ok {
				continue
			}
			violations = append(violations,
				fmt.Sprintf("%s: struct %s carries Tracer trace.Tracer but no Meter metric.Meter sibling",
					pos.String(), st.spec.Name.Name))
		}
	}

	for key := range allow {
		if _, ok := seen[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	sort.Strings(violations)
	return violations, seen, stale, nil
}

type structEntry struct {
	spec *ast.TypeSpec
	st   *ast.StructType
}

type pkgIndex struct {
	dir     string
	structs map[string]structEntry
	aliases map[string]aliasTarget
}

type aliasTarget struct {
	pkg, name string
	localRef  string
}

func loadStructIndex(root string, fset *token.FileSet) (map[string]*pkgIndex, error) {
	pkgs := map[string]*pkgIndex{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
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
		dir := filepath.Dir(path)
		pkg := pkgs[dir]
		if pkg == nil {
			pkg = &pkgIndex{dir: dir, structs: map[string]structEntry{}, aliases: map[string]aliasTarget{}}
			pkgs[dir] = pkg
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				pkg.structs[ts.Name.Name] = structEntry{spec: ts, st: st}
				return true
			}
			if ts.Assign != token.NoPos {
				if sel, ok := ts.Type.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok {
						pkg.aliases[ts.Name.Name] = aliasTarget{pkg: id.Name, name: sel.Sel.Name}
					}
				} else if id, ok := ts.Type.(*ast.Ident); ok {
					pkg.aliases[ts.Name.Name] = aliasTarget{localRef: id.Name}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pkgs, nil
}

func pkgHasPair(pkg *pkgIndex, structName string, visiting map[string]bool) (hasTracer, hasMeter bool) {
	if visiting[structName] {
		return false, false
	}
	entry, ok := pkg.structs[structName]
	if !ok {
		return false, false
	}
	visiting[structName] = true
	defer delete(visiting, structName)

	if entry.st.Fields == nil {
		return false, false
	}
	for _, field := range entry.st.Fields.List {
		if len(field.Names) == 0 {
			embeddedName := embeddedTypeName(field.Type)
			if embeddedName == "" {
				continue
			}
			et, em := pkgHasPair(pkg, embeddedName, visiting)
			hasTracer = hasTracer || et
			hasMeter = hasMeter || em
			continue
		}
		t, m := fieldMatches(pkg, field)
		hasTracer = hasTracer || t
		hasMeter = hasMeter || m
	}
	return hasTracer, hasMeter
}

func embeddedTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func fieldMatches(pkg *pkgIndex, field *ast.Field) (hasTracer, hasMeter bool) {
	for _, name := range field.Names {
		switch name.Name {
		case "Tracer":
			if exprResolvesTo(pkg, field.Type, "trace", "Tracer") {
				hasTracer = true
			}
		case "Meter":
			if exprResolvesTo(pkg, field.Type, "metric", "Meter") {
				hasMeter = true
			}
		}
	}
	return hasTracer, hasMeter
}

func exprResolvesTo(pkg *pkgIndex, expr ast.Expr, pkgName, typeName string) bool {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		id, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}
		return id.Name == pkgName && t.Sel.Name == typeName
	case *ast.Ident:
		alias, ok := pkg.aliases[t.Name]
		if !ok {
			return false
		}
		if alias.pkg == pkgName && alias.name == typeName {
			return true
		}
		if alias.localRef != "" {
			if next, ok := pkg.aliases[alias.localRef]; ok {
				return next.pkg == pkgName && next.name == typeName
			}
		}
	}
	return false
}

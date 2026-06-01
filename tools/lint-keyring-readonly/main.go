// Command lint-keyring-readonly scans Go source under the supplied
// roots and rejects any function-call expression with name `KeyringSet`
// (or matching the keyring-mutate suffix family) issued outside an
// allowlisted bootstrap function — `init` / `Setup` / `LoadKeyring`.
//
// Spec §5: the keyring is loaded at process Setup and is read-only
// afterwards. A runtime mutation path lets a compromised process inject
// a hostile key and forge events that pass verification.
//
// Exit codes: 0 = clean, 1 = findings. Findings print on stdout as
// `file:line: KeyringSet outside boot path (enclosing func: X)`.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// allowedEnclosers names the functions inside which KeyringSet is legal.
// init and Setup are the established boot-time hooks. LoadKeyring is
// the typical helper name in keyring-implementing packages.
var allowedEnclosers = map[string]bool{
	"init":        true,
	"Setup":       true,
	"LoadKeyring": true,
	"TestMain":    true,
}

// mutateNames are function/method names whose call is treated as a
// keyring mutation. Matching is on the rightmost identifier of the call
// expression (so `keyring.Set` and `k.KeyringSet` both match).
var mutateNames = map[string]bool{
	"KeyringSet": true,
	"SetKey":     true,
	"AddKey":     true,
}

type finding struct {
	File     string
	Line     int
	Call     string
	Enclosed string
}

func (f finding) String() string {
	enc := f.Enclosed
	if enc == "" {
		enc = "(file scope)"
	}
	return fmt.Sprintf("%s:%d: %s outside boot path (enclosing func: %s)",
		f.File, f.Line, f.Call, enc)
}

func main() {
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lint-keyring-readonly <root> [<root>...]")
		os.Exit(2)
	}
	var all []finding
	for _, r := range roots {
		fs, err := runLinter(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lint %s: %v\n", r, err)
			os.Exit(2)
		}
		all = append(all, fs...)
	}
	if len(all) == 0 {
		return
	}
	for _, f := range all {
		fmt.Println(f.String())
	}
	os.Exit(1)
}

// runLinter walks root, parses every *.go file (skipping vendor /
// testdata / .git / node_modules), and emits a finding for every
// mutate-named call whose enclosing function is not allowlisted.
func runLinter(root string) ([]finding, error) {
	var out []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			// Tests set fixture keys routinely; the lint targets
			// production paths only.
			return nil
		}
		fs, err := scanFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, fs...)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

func scanFile(path string) ([]finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		funcName := fn.Name.Name
		allowed := allowedEnclosers[funcName]
		if fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calledName(call.Fun)
			if !mutateNames[name] {
				return true
			}
			if allowed {
				return true
			}
			pos := fset.Position(call.Pos())
			out = append(out, finding{
				File:     path,
				Line:     pos.Line,
				Call:     name,
				Enclosed: funcName,
			})
			return true
		})
	}
	return out, nil
}

// calledName extracts the rightmost identifier from a call's Fun expr.
// Handles bare ident (`KeyringSet(...)`), selector (`keyring.Set(...)`),
// and method on indexed/composite (`k.Set(...)`). Returns "" if the
// shape is unsupported (rare; e.g. function literals).
func calledName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	default:
		return ""
	}
}

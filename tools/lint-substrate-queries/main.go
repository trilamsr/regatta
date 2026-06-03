// Command lint-substrate-queries scans Go source under the supplied
// roots and rejects any SQL string literal that selects from
// substrate_events without a scoped WHERE clause.
//
// Rules (spec §6):
//  1. Every read from substrate_events MUST carry `kind = ?` in WHERE.
//  2. Cross-run reads (no `run_id = ?` in WHERE) MUST also carry
//     `tenant_id = ?`.
//
// Exceptions:
//   - The substrate package itself (internal/orchestrator/state/substrate/)
//     implements the primitive and runs internal PK / supersedes scans
//     that are not consumer-facing reads. Skipped.
//   - Test files (*_test.go) are not policed; tests may legitimately
//     probe migration shape or back-door queries.
//   - PK lookups (`WHERE id = ?`) are exempt: they retrieve a single
//     event by ULID, never a scan.
//   - PRAGMA / introspection (pragma_table_info etc.) is exempt.
//
// Exit codes: 0 = clean, 1 = findings. Findings print one per line to
// stdout in `file:line: reason: snippet` form so editors can navigate them.
//
// Boundary: this pass is AST-only — string-constant identifiers in a
// concat chain (e.g. `const tableName = "substrate_events"`) are NOT
// resolved through `go/types` / `go/packages`. The union we classify is
// the bag of *string-literal* operands present in the ADD-chain. If a
// caller hides every load-bearing token behind a non-literal binding,
// the lint will under-report. A future pass can fold in go/packages
// when callers materialize.
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
	"regexp"
	"strings"
)

// finding is one violating SQL string literal.
type finding struct {
	File    string
	Line    int
	Snippet string
	Reason  string
}

func (f finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %q", f.File, f.Line, f.Reason, f.Snippet)
}

func main() {
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lint-substrate-queries <root> [<root>...]")
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

// runLinter walks root and returns every violating SQL literal. I/O
// and parse failures surface as err; lint findings come back as the
// slice with err==nil.
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
			rel, _ := filepath.Rel(root, path)
			if isSubstratePackage(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
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

// isSubstratePackage returns true when rel is the substrate
// implementation directory (relative to a walked root). The lint
// exempts internal cycle-check + PK lookup queries authored inside the
// primitive itself; only callers need policing.
func isSubstratePackage(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == "internal/orchestrator/state/substrate" ||
		strings.HasSuffix(rel, "/orchestrator/state/substrate") ||
		rel == "orchestrator/state/substrate" ||
		rel == "state/substrate" ||
		rel == "substrate"
}

// scanFile parses one Go file and returns every violating SQL literal.
// Per-literal scanning misses `"FROM " + tableConst + " WHERE " + scope`
// — the substrate_events token sits alone and trips kind=? in isolation
// (#234). First pass classifies the union of literal operands in each
// top-level ADD-chain; second pass catches standalone literals.
func scanFile(path string) ([]finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []finding
	seen := map[*ast.BasicLit]bool{}
	handledExpr := map[*ast.BinaryExpr]bool{}

	ast.Inspect(f, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != token.ADD {
			return true
		}
		if handledExpr[be] {
			return true
		}
		if !containsStringLit(be) {
			return true
		}
		lits := collectStringConcatLits(be)
		if len(lits) == 0 {
			return true
		}
		// Children of this root chain must be skipped — ast.Inspect
		// descends into them next.
		markADDs(be, handledExpr)
		var b strings.Builder
		for _, l := range lits {
			seen[l] = true
			b.WriteString(unquote(l.Value))
			b.WriteByte(' ')
		}
		union := b.String()
		if !mentionsSubstrateEvents(union) {
			return true
		}
		if reason, bad := classify(union); bad {
			pos := fset.Position(be.Pos())
			out = append(out, finding{
				File:    path,
				Line:    pos.Line,
				Snippet: collapseWhitespace(union),
				Reason:  reason + " (concat)",
			})
		}
		return true
	})

	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if seen[lit] {
			return true
		}
		s := unquote(lit.Value)
		if s == "" || !mentionsSubstrateEvents(s) {
			return true
		}
		if reason, bad := classify(s); bad {
			pos := fset.Position(lit.Pos())
			out = append(out, finding{
				File:    path,
				Line:    pos.Line,
				Snippet: collapseWhitespace(s),
				Reason:  reason,
			})
		}
		return true
	})
	return out, nil
}

// markADDs records every nested ADD BinaryExpr under expr so the
// outer-pass walker skips them after the root chain is classified.
func markADDs(expr ast.Expr, set map[*ast.BinaryExpr]bool) {
	be, ok := expr.(*ast.BinaryExpr)
	if !ok || be.Op != token.ADD {
		return
	}
	set[be] = true
	markADDs(be.X, set)
	markADDs(be.Y, set)
}

// containsStringLit returns true when expr has at least one STRING
// BasicLit somewhere inside an ADD-chain. Non-ADD nodes terminate
// recursion.
func containsStringLit(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return false
		}
		return containsStringLit(e.X) || containsStringLit(e.Y)
	}
	return false
}

// collectStringConcatLits walks an ADD-chain and returns every STRING
// BasicLit operand. Non-literal operands (idents, calls, etc.) are
// dropped — the union represents the bytes we can see at lint time.
func collectStringConcatLits(expr ast.Expr) []*ast.BasicLit {
	var out []*ast.BasicLit
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				out = append(out, v)
			}
		case *ast.BinaryExpr:
			if v.Op != token.ADD {
				return
			}
			walk(v.X)
			walk(v.Y)
		}
	}
	walk(expr)
	return out
}

// unquote strips surrounding quotes/backticks from a raw BasicLit.Value
// and returns "" when the input is shorter than 2 bytes.
func unquote(raw string) string {
	if len(raw) < 2 {
		return ""
	}
	return raw[1 : len(raw)-1]
}

// mentionsSubstrateEvents catches the table name in any case and inside
// backtick-bounded heredocs. Word boundaries prevent matches inside
// identifiers like "x_substrate_events_y".
var substrateRE = regexp.MustCompile(`(?i)\bsubstrate_events\b`)

func mentionsSubstrateEvents(s string) bool {
	return substrateRE.MatchString(s)
}

var (
	kindRE     = regexp.MustCompile(`(?i)\bkind\s*=\s*\?`)
	runIDRE    = regexp.MustCompile(`(?i)\brun_id\s*=\s*\?`)
	tenantRE   = regexp.MustCompile(`(?i)\btenant_id\s*=\s*\?`)
	pkLookupRE = regexp.MustCompile(`(?i)\bid\s*=\s*\?`)
	notReadRE  = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|PRAGMA)\b`)
)

// classify inspects an SQL string that mentions substrate_events and
// returns (reason, true) when the string violates the lint rule.
func classify(s string) (string, bool) {
	if notReadRE.MatchString(s) {
		return "", false
	}
	// PRAGMA-style introspection passes the table name as a function
	// argument; treat as non-read.
	if strings.Contains(strings.ToLower(s), "pragma_") {
		return "", false
	}
	// PK lookup: `WHERE id = ?` retrieves a single row.
	if pkLookupRE.MatchString(s) {
		return "", false
	}
	if !kindRE.MatchString(s) {
		return "missing `kind = ?` filter", true
	}
	// Has kind=?. If run_id=? is absent (cross-run read), require tenant_id=?.
	if !runIDRE.MatchString(s) {
		if !tenantRE.MatchString(s) {
			return "cross-run read missing `tenant_id = ?` filter", true
		}
	}
	return "", false
}

// collapseWhitespace replaces runs of whitespace with a single space
// for snippet display.
func collapseWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

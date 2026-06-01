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
func scanFile(path string) ([]finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []finding
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s := lit.Value
		if len(s) < 2 {
			return true
		}
		// Strip surrounding quote / backtick.
		s = s[1 : len(s)-1]
		if !mentionsSubstrateEvents(s) {
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

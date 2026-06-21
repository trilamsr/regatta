// Command grep-go-ident reads Go source on stdin and reports whether any
// of the symbol names passed as arguments appears as a real identifier
// token, not inside a comment, string/char literal, or raw-string.
//
// It backs check-tdd.sh's cross-package symbol-match. The prior
// implementation stripped line comments + string literals with per-line
// awk and then ran `grep -qw`, which left block comments (`/* Sym */`,
// including multi-line spans) and any awk-strip edge case as a bypass:
// a test mentioning a prod symbol only in a comment satisfied the gate
// without a real reference (BUG-1058 / #1058, reopen of #1057). A
// token-level scan closes every literal/comment class at once because
// go/scanner classifies comments and literals as non-IDENT tokens.
//
// Exit codes: 0 = at least one symbol found as an IDENT; 1 = none found
// (or no symbols passed); 2 = usage / read error. The found symbol name
// is printed on stdout so callers can log which reference satisfied the
// gate.
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"os"
)

func main() {
	symbols := os.Args[1:]
	if len(symbols) == 0 {
		fmt.Fprintln(os.Stderr, "grep-go-ident: usage: grep-go-ident SYMBOL [SYMBOL...] < file.go")
		os.Exit(2)
	}
	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grep-go-ident: read stdin: %v\n", err)
		os.Exit(2)
	}
	if match, ok := firstIdentMatch(src, symbols); ok {
		fmt.Println(match)
		os.Exit(0)
	}
	os.Exit(1)
}

// firstIdentMatch reports the first symbol from want that the scanner
// emits as a token.IDENT in src. Comments are excluded (ScanComments is
// not set) so /* ... */ and // ... mentions never count; string, char,
// and raw-string literals scan as token.STRING/token.CHAR, never IDENT,
// so quoted mentions never count either. Lexically malformed input is
// tolerated: the scanner's error handler is a no-op and partial token
// streams still surface any genuine identifier.
func firstIdentMatch(src []byte, want []string) (string, bool) {
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}
	fset := token.NewFileSet()
	file := fset.AddFile("stdin.go", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, 0)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return "", false
		}
		if tok == token.IDENT && wantSet[lit] {
			return lit, true
		}
	}
}

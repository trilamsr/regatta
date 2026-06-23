// L0-gate entrypoints: diff, refs (merge-base), and merge-commit modes
// share parser + emitter, so they live in one file.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/l0"
)

func runL0(args []string) int {
	fs := flag.NewFlagSet("l0", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta l0 <diff-file>  ('-' for stdin)")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	var data []byte
	var err error
	if fs.Arg(0) == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(fs.Arg(0))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta l0:", err)
		return 2
	}
	result := l0.Check(l0.Default(), l0.ParseUnifiedDiff(string(data)))
	return emitL0(result)
}

func runL0Refs(args []string) int {
	fs := flag.NewFlagSet("l0-refs", flag.ContinueOnError)
	repoDir := fs.String("repo", ".", "Path to the git repository")
	baseRef := fs.String("base", "", "Base ref (branch, tag, or sha)")
	headRef := fs.String("head", "", "Head ref (branch, tag, or sha)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *baseRef == "" || *headRef == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta l0-refs: -base and -head required")
		return 2
	}
	result, err := l0.CheckRefs(context.Background(), l0.Default(), *repoDir, *baseRef, *headRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta l0-refs:", err)
		return 2
	}
	return emitL0(result)
}

func runL0Merge(args []string) int {
	fs := flag.NewFlagSet("l0-merge", flag.ContinueOnError)
	repoDir := fs.String("repo", ".", "Path to the git repository")
	commit := fs.String("commit", "", "Merge commit sha")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *commit == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta l0-merge: -commit required")
		return 2
	}
	result, err := l0.CheckMergeCommit(context.Background(), l0.Default(), *repoDir, *commit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta l0-merge:", err)
		return 2
	}
	return emitL0(result)
}

func emitL0(result schemas.GateResult) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
	if result.Verdict != schemas.VerdictPass {
		return 1
	}
	return 0
}
